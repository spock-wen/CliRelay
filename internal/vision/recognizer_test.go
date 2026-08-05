package vision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecognizerSendsPreprocessedImageAndParsesSummary(t *testing.T) {
	var gotImageURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if user, ok := msgs[len(msgs)-1].(map[string]any); ok {
				if content, ok := user["content"].([]any); ok {
					for _, c := range content {
						cm, ok := c.(map[string]any)
						if !ok {
							continue
						}
						if cm["type"] == "image_url" {
							if iu, ok := cm["image_url"].(map[string]any); ok {
								gotImageURL, _ = iu["url"].(string)
							}
						}
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: A red button.\nOCR: SUBMIT\nLAYOUT: Button center\nDETAILS: highlighted"}}]}`))
	}))
	defer srv.Close()

	preCfg := DefaultPreprocessConfig()
	preCfg.StandardMaxDim = 64 // force downscale of the 1024x512 fixture so the payload is not full-size
	r := NewRecognizer(RecognizerConfig{
		BaseURL:            srv.URL,
		APIKeys:            []string{"k1"},
		Model:              "kimi-k2.6",
		MaxConcurrency:     10,
		PerKeyConcurrency:  5,
		KeyCooldown:        time.Minute,
		Timeout:            5 * time.Second,
		Retries:            0,
		Preprocess:         preCfg,
		AnalyzeTimeout:     5 * time.Second,
	})
	resp, err := r.Analyze(context.Background(), AnalyzeRequest{
		ImageData: base64Of(t, makeTestJPEG(t, 1024, 512)),
		MIMEType:  "image/jpeg",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !strings.Contains(resp.Summary.Summary, "red button") {
		t.Fatalf("summary = %q, want it to mention red button", resp.Summary.Summary)
	}
	if len(resp.Summary.OCRHints) == 0 || resp.Summary.OCRHints[0] != "SUBMIT" {
		t.Fatalf("OCR hints = %v, want [SUBMIT]", resp.Summary.OCRHints)
	}
	if gotImageURL == "" {
		t.Fatal("no image_url sent")
	}
	if len(gotImageURL) > 2000 {
		t.Fatal("image_url appears to carry the full-size image")
	}
}

func TestRecognizerExhaustedKeysReturnsError(t *testing.T) {
	// Single key with per-key concurrency 1; the first request blocks the key,
	// so the second must fail fast with "no kimi key available".
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: x"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 1, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 0, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	ctx := context.Background()
	firstDone := make(chan struct{})
	go func() {
		_, _ = r.Analyze(ctx, AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"})
		close(firstDone)
	}()
	// Give the first request time to acquire the key.
	time.Sleep(30 * time.Millisecond)
	if _, err := r.Analyze(ctx, AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err == nil {
		t.Fatal("expected error when no key available")
	}
	close(blocked)
	<-firstDone
}

func TestRecognizerReAcquiresFreshKeyAfterRetryableError(t *testing.T) {
	// k1 always 429s; k2 succeeds. After the retryable error the key must be
	// released and cooled so the retry round-robins to a fresh key (k2) —
	// holding the throttled key across retries would make the retry fail too.
	var hitsK1, hitsK2 int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.Header.Get("Authorization"), "k1"):
			atomic.AddInt32(&hitsK1, 1)
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case strings.Contains(r.Header.Get("Authorization"), "k2"):
			atomic.AddInt32(&hitsK2, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: ok"}}]}`))
		default:
			t.Fatalf("unexpected Authorization %q", r.Header.Get("Authorization"))
		}
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1", "k2"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 5, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 1, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	if _, err := r.Analyze(context.Background(), AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := atomic.LoadInt32(&hitsK1); got != 1 {
		t.Fatalf("k1 hit %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&hitsK2); got != 1 {
		t.Fatalf("k2 hit %d times, want 1 (fresh key must be re-acquired after 429)", got)
	}
}

func TestRecognizerDoesNotRetryNonRetryableError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 5, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 2, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	if _, err := r.Analyze(context.Background(), AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err == nil {
		t.Fatal("expected error from 400 response")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("upstream hit %d times, want 1 (non-retryable 400 must not be retried)", got)
	}
}

func TestRecognizerDoesNotRetry400Containing429Substring(t *testing.T) {
	// Retryability must be decided by HTTP status, not by substring-matching
	// the response body: a 400 whose JSON mentions a 429 upstream code must
	// NOT be retried.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limit_429_exhausted","message":"limit exceeded upstream"}}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 5, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 2, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	if _, err := r.Analyze(context.Background(), AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err == nil {
		t.Fatal("expected error from 400 response")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("upstream hit %d times, want 1 (400 with '429' in body must not be retried)", got)
	}
}

func TestRecognizerRetries500WithoutCoolingKey(t *testing.T) {
	// A 5xx must be retried WITHOUT cooling the key: cooling on 5xx would black
	// out a single-key pool for the whole cooldown window even after the
	// upstream recovers. Only 429 is key-specific throttling that cools.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			http.Error(w, "temporary server trouble", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: ok"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 5, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 2, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	if _, err := r.Analyze(context.Background(), AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("upstream hit %d times, want 2 (single-key pool must retry a 5xx on the same key)", got)
	}
}

func TestRecognizerRetriesTransientNetworkError(t *testing.T) {
	// First attempt: connection is closed without an HTTP response (a transient
	// network blip). A transport error is retryable and must not cool the key,
	// so a single-key pool can still succeed on the next attempt.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: recovered"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 5, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 2, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	if _, err := r.Analyze(context.Background(), AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("upstream hit %d times, want 2 (transient network error must be retried)", got)
	}
}

package vision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

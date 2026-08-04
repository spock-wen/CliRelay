package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RecognizerConfig struct {
	BaseURL           string
	APIKeys           []string
	Model             string
	MaxConcurrency    int
	PerKeyConcurrency int
	KeyCooldown       time.Duration
	Timeout           time.Duration
	Retries           int
	Preprocess        PreprocessConfig
	AnalyzeTimeout    time.Duration
}

type Recognizer struct {
	cfg     RecognizerConfig
	limiter *ConcurrencyLimiter
	pool    *KeyPool
	client  *http.Client
}

func NewRecognizer(cfg RecognizerConfig) *Recognizer {
	limMax := cfg.MaxConcurrency
	if limMax <= 0 {
		limMax = 100
	}
	return &Recognizer{
		cfg:     cfg,
		limiter: NewConcurrencyLimiter(limMax),
		pool:    NewKeyPool(cfg.APIKeys, cfg.PerKeyConcurrency, cfg.KeyCooldown),
		client:  &http.Client{Timeout: cfg.Timeout},
	}
}

func (r *Recognizer) Name() string { return "recognizer" }

func (r *Recognizer) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResponse, error) {
	raw, err := base64.StdEncoding.DecodeString(req.ImageData)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("decode image data: %w", err)
	}
	proc, err := PreprocessImage(raw, PreprocessModeStandard, r.cfg.Preprocess)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("preprocess image: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.AnalyzeTimeout)
	defer cancel()

	var resp AnalyzeResponse
	err = r.limiter.Run(ctx, func() error {
		key, release, ok := r.pool.Acquire()
		if !ok {
			return fmt.Errorf("no kimi key available")
		}
		defer release()

		body := r.buildBody(proc.Base64, req)
		var lastErr error
		for attempt := 0; attempt <= r.cfg.Retries; attempt++ {
			out, err := r.doRequest(ctx, key, body)
			if err == nil {
				parsed, perr := r.parseSummary(out, req)
				if perr != nil {
					return perr
				}
				resp = parsed
				return nil
			}
			lastErr = err
			if !isRetryable(err) {
				break
			}
			r.pool.MarkUnavailable(key)
		}
		return lastErr
	})
	return resp, err
}

func (r *Recognizer) buildBody(imageData string, req AnalyzeRequest) []byte {
	prompt := "You are an image analyzer for a coding assistant. Describe this image in detail, focusing on:\n\nSUMMARY: 1-2 sentence overall description of what this image shows.\nOCR: Any text visible in the image (error messages, code, UI labels).\nLAYOUT: The layout structure, key visual elements, their relative positions.\nDETAILS: Any other notable details (colors, icons, UI state, highlighted elements).\n\nBe thorough — the model receiving this data cannot see the original image."
	if req.IsFollowUp && req.Existing.Summary != "" {
		prompt = fmt.Sprintf("You previously analyzed this image:\nSUMMARY: %s\nOCR: %s\nLAYOUT: %s\nDETAILS: %s\n\nThe user asks: %q\nProvide ONLY new supplementary info not covered above.", req.Existing.Summary, strings.Join(req.Existing.OCRHints, "; "), strings.Join(req.Existing.LayoutHints, "; "), strings.Join(req.Existing.DetailHints, "; "), req.Query)
	}
	dataURL := "data:image/jpeg;base64," + imageData
	body := map[string]any{
		"model": r.cfg.Model,
		"messages": []map[string]any{
			{"role": "system", "content": "You are an expert image analyst. Provide structured, detailed descriptions."},
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]any{"url": dataURL}},
			}},
		},
		"max_tokens": 1024,
	}
	out, _ := json.Marshal(body)
	return out
}

func (r *Recognizer) doRequest(ctx context.Context, apiKey string, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API status %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}

func (r *Recognizer) parseSummary(data []byte, req AnalyzeRequest) (AnalyzeResponse, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return AnalyzeResponse{}, err
	}
	if len(raw.Choices) == 0 {
		return AnalyzeResponse{}, fmt.Errorf("empty choices")
	}
	return AnalyzeResponse{Summary: parseStructuredResponse(raw.Choices[0].Message.Content, req)}, nil
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "502") || strings.Contains(s, "503")
}

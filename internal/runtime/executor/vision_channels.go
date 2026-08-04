package executor

import (
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
)

// visionChannelBaseURL resolves the shared base URL and all API keys of a
// named channel (e.g. "xunfei-199", which carries 10 kimi keys).
func visionChannelBaseURL(cfg *config.Config, channel string) (string, []string) {
	if cfg == nil {
		return "", nil
	}
	var keys []string
	baseURL := ""
	for _, k := range cfg.ClaudeKey {
		if !strings.EqualFold(strings.TrimSpace(k.Name), channel) {
			continue
		}
		if baseURL == "" && k.BaseURL != "" {
			baseURL = k.BaseURL
		}
		if k.APIKey != "" {
			keys = append(keys, k.APIKey)
		}
	}
	return baseURL, keys
}

// newVisionRecognizer returns the process-wide kimi recognizer, building it
// lazily and caching it so the global concurrency limiter, key pool (cooldown),
// and HTTP connection pool are shared across all requests. Returns nil when
// vision is disabled or the configured channel is missing. A failed build is
// NOT cached, so a later call can retry construction (e.g. after config fix).
func (e *ClaudeExecutor) newVisionRecognizer() *vision.Recognizer {
	if e == nil || e.cfg == nil || !e.cfg.Vision.Enabled {
		return nil
	}
	e.recMu.Lock()
	defer e.recMu.Unlock()
	if e.rec != nil {
		return e.rec
	}
	r := e.buildVisionRecognizer()
	if r != nil {
		e.rec = r
	}
	return r
}

// buildVisionRecognizer constructs a fresh recognizer from the current vision
// config. Returns nil when the configured channel carries no base URL/keys.
func (e *ClaudeExecutor) buildVisionRecognizer() *vision.Recognizer {
	v := e.cfg.Vision
	baseURL, keys := visionChannelBaseURL(e.cfg, v.Channel)
	if baseURL == "" || len(keys) == 0 {
		return nil
	}
	return vision.NewRecognizer(vision.RecognizerConfig{
		BaseURL:           baseURL,
		APIKeys:           keys,
		Model:             v.Model,
		MaxConcurrency:    v.MaxConcurrency,
		PerKeyConcurrency: v.PerKeyConcurrency,
		KeyCooldown:       time.Duration(v.KeyCooldownMs) * time.Millisecond,
		Timeout:           30 * time.Second,
		Retries:           v.Retries,
		Preprocess: vision.PreprocessConfig{
			MaxSizeBytes:   v.MaxSizeMB * 1024 * 1024,
			StandardMaxDim: v.MaxDimension,
			OCRMaxDim:      v.OCRMaxDimension,
			DiffMaxDim:     v.MaxDimension,
			JPEGQuality:    v.JPEGQuality,
		},
		AnalyzeTimeout: time.Duration(v.AnalyzeTimeoutMs) * time.Millisecond,
	})
}

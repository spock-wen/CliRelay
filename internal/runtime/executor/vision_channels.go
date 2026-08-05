package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
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

// visionRecognizerKey fingerprints every input that shapes a recognizer so a
// config reload that changes any of them builds a fresh instance instead of
// silently reusing a stale cached one. The resolved base-url + keys (from
// vision.base-url/api-keys or the channel fallback) are included verbatim.
func visionRecognizerKey(cfg *config.Config) string {
	v := cfg.Vision
	baseURL, keys := v.BaseURL, v.APIKeys
	if baseURL == "" || len(keys) == 0 {
		// Fallback: resolve from a configured config.yaml channel (legacy path).
		baseURL, keys = visionChannelBaseURL(cfg, v.Channel)
	}
	h := sha256.New()
	h.Write([]byte(baseURL))
	for _, k := range keys {
		h.Write([]byte{0})
		h.Write([]byte(k))
	}
	fmt.Fprintf(h, "|%s|%d|%d|%d|%d|%d|%d|%d|%d|%d",
		v.Model, v.MaxConcurrency, v.PerKeyConcurrency, v.KeyCooldownMs,
		v.MaxSizeMB, v.MaxDimension, v.OCRMaxDimension, v.JPEGQuality,
		v.AnalyzeTimeoutMs, v.Retries)
	return hex.EncodeToString(h.Sum(nil))
}

var (
	visionSharedRecMu  sync.Mutex
	visionSharedRecKey string
	visionSharedRec    *vision.Recognizer
)

// sharedVisionRecognizer returns the process-wide kimi recognizer, building it
// lazily and caching it so the global concurrency limiter, key pool (cooldown),
// and HTTP connection pool are shared across ALL requests and ALL executors
// (ClaudeExecutor, OpenAICompatExecutor, ...) — not one pool per executor.
// Returns nil when vision is disabled or no base URL + keys resolve. A failed
// build is NOT cached, so a later call can retry construction (e.g. after a
// config fix); a config reload that changes the resolved inputs rebuilds it.
func sharedVisionRecognizer(cfg *config.Config) *vision.Recognizer {
	if cfg == nil || !cfg.Vision.Enabled {
		return nil
	}
	key := visionRecognizerKey(cfg)
	visionSharedRecMu.Lock()
	defer visionSharedRecMu.Unlock()
	if visionSharedRec != nil && visionSharedRecKey == key {
		return visionSharedRec
	}
	r := buildVisionRecognizer(cfg)
	if r == nil {
		return nil
	}
	visionSharedRec, visionSharedRecKey = r, key
	return r
}

// buildVisionRecognizer constructs a fresh recognizer from the current vision
// config. It prefers the vision section's own base-url + api-keys (self-contained,
// independent of the channel system), falling back to a config.yaml channel.
// Returns nil when neither source yields a base URL and keys.
func buildVisionRecognizer(cfg *config.Config) *vision.Recognizer {
	if cfg == nil {
		return nil
	}
	v := cfg.Vision
	baseURL, keys := v.BaseURL, v.APIKeys
	if baseURL == "" || len(keys) == 0 {
		// Fallback: resolve from a configured config.yaml channel (legacy path).
		baseURL, keys = visionChannelBaseURL(cfg, v.Channel)
	}
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

// newVisionRecognizer keeps the per-executor accessor shape used across the
// codebase and tests, delegating to the process-wide shared instance so both
// executors share the same concurrency-limited key pool.
func (e *ClaudeExecutor) newVisionRecognizer() *vision.Recognizer {
	if e == nil {
		return nil
	}
	return sharedVisionRecognizer(e.cfg)
}

func (e *OpenAICompatExecutor) newVisionRecognizer() *vision.Recognizer {
	if e == nil {
		return nil
	}
	return sharedVisionRecognizer(e.cfg)
}

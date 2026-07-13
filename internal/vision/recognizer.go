package vision

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

// RecognitionTarget is the parsed recognition model call parameters.
type RecognitionTarget struct {
	BaseURL string
	APIKey  string
	Model   string
}

// ResolveRecognitionTarget parses "provider-name/model-name" into callable recognition target.
// Provider name matches config.OpenAICompatibility[i].Name (case-insensitive).
// APIKey takes the first non-empty APIKeyEntries entry.
// Returns ok=false on parse failure.
func ResolveRecognitionTarget(cfg *config.Config, spec string) (*RecognitionTarget, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" || cfg == nil {
		return nil, false
	}
	idx := strings.IndexByte(spec, '/')
	if idx <= 0 || idx >= len(spec)-1 {
		return nil, false
	}
	providerName := strings.TrimSpace(spec[:idx])
	model := strings.TrimSpace(spec[idx+1:])
	if providerName == "" || model == "" {
		return nil, false
	}
	for i := range cfg.OpenAICompatibility {
		compat := &cfg.OpenAICompatibility[i]
		if !strings.EqualFold(strings.TrimSpace(compat.Name), providerName) {
			continue
		}
		apiKey := ""
		for _, entry := range compat.APIKeyEntries {
			if strings.TrimSpace(entry.APIKey) != "" {
				apiKey = strings.TrimSpace(entry.APIKey)
				break
			}
		}
		if apiKey == "" {
			return nil, false
		}
		return &RecognitionTarget{
			BaseURL: strings.TrimSpace(compat.BaseURL),
			APIKey:  apiKey,
			Model:   model,
		}, true
	}
	return nil, false
}

// imageAnalysisCache holds previously analyzed image text, keyed by image data hash.
// On compact, historical images can reuse their earlier analysis instead of a bare placeholder.
// Uses approximate LRU: when maxCacheEntries is exceeded, evicts the oldest entries
// down to keepCacheEntries.
const (
	maxCacheEntries  = 2000
	keepCacheEntries = 1500
)

type cacheEntry struct {
	text     string
	storedAt int64 // unix nano
}

var (
	imageAnalysisCache sync.Map     // string(hash) → cacheEntry
	cacheEntryCount    atomic.Int64 // approximate count
)

func cacheLoad(key string) (string, bool) {
	v, ok := imageAnalysisCache.Load(key)
	if !ok {
		return "", false
	}
	return v.(cacheEntry).text, true
}

func cacheStore(key, value string) {
	now := time.Now().UnixNano()
	_, existed := imageAnalysisCache.Swap(key, cacheEntry{text: value, storedAt: now})
	if !existed {
		if cacheEntryCount.Add(1) > maxCacheEntries {
			evictOldest(keepCacheEntries)
		}
	}
}

func evictOldest(keep int) {
	type kv struct {
		key      string
		storedAt int64
	}
	entries := make([]kv, 0, maxCacheEntries)
	imageAnalysisCache.Range(func(k, v any) bool {
		entries = append(entries, kv{key: k.(string), storedAt: v.(cacheEntry).storedAt})
		return true
	})
	if len(entries) <= keep {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].storedAt < entries[j].storedAt })
	removed := 0
	for i := 0; i < len(entries)-keep; i++ {
		imageAnalysisCache.Delete(entries[i].key)
		removed++
	}
	cacheEntryCount.Store(int64(len(entries) - removed))
	log.Warnf("vision: image analysis cache LRU evicted %d entries (%d → %d)", removed, len(entries), len(entries)-removed)
}

// RecognizeImagesResult is the outcome of image recognition backfill.
type RecognizeImagesResult struct {
	Payload       []byte
	Applied       bool
	FallbackModel string // recognition model name for usage logging
}

// RecognizeCurrentTurnImages performs recognition backfill on current-turn images
// and replaces historical images with a placeholder so text-only upstream models
// never receive image content parts.
//   - Skips if analyzer is nil, no images at all, or current model supports native vision.
//   - Current-turn images: calls analyzer.Analyze, replaces with text summary.
//   - Historical images: replaces with "[图片内容]" placeholder with no analyzer call.
//   - Returns modified payload with Applied=true and FallbackModel=analyzer.Name().
func RecognizeCurrentTurnImages(ctx context.Context, analyzer ImageAnalyzer, payload []byte, currentModel string) RecognizeImagesResult {
	result := RecognizeImagesResult{Payload: payload}
	if analyzer == nil {
		return result
	}
	walk := WalkPayload(payload)
	if len(walk.Parts) == 0 {
		return result
	}
	if SupportsVisionByModelName(currentModel) {
		return result
	}

	for _, ip := range walk.Parts {
		var placeholder string
		if ip.IsCurrent {
			placeholder = recognizeOneImage(ctx, analyzer, ip)
		} else {
			hash := ComputeHash(ip.Data)
			if cached, ok := cacheLoad(string(hash)); ok {
				placeholder = cached
			} else {
				placeholder = "[图片内容]"
			}
		}
		newPayload, err := ReplaceImagePartEx(payload, ip, placeholder, ip.ArrayName)
		if err != nil {
			log.Errorf("vision: replace image part failed: %v", err)
			continue
		}
		payload = newPayload
	}

	result.Payload = payload
	result.Applied = true
	result.FallbackModel = analyzer.Name()
	return result
}

func recognizeOneImage(ctx context.Context, analyzer ImageAnalyzer, ip ImagePart) string {
	req := AnalyzeRequest{
		ImageData:  ip.Data,
		ImageURL:   ip.RemoteURL,
		MIMEType:   ip.MIMEType,
		SourceKind: ImageSourceUserUpload,
		TurnIndex:  0,
	}
	resp, err := analyzer.Analyze(ctx, req)
	if err != nil {
		log.Errorf("vision: analyze image failed: %v", err)
		return "[图片识别失败]"
	}
	text := RenderSummary(resp.Summary)
	if strings.TrimSpace(text) == "" {
		return "[图片识别失败]"
	}
	placeholder := "[图片内容] " + text
	if ip.Data != "" {
		hash := ComputeHash(ip.Data)
		cacheStore(string(hash), placeholder)
	}
	return placeholder
}

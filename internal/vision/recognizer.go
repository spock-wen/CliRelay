package vision

import (
	"context"
	"strings"

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

// RecognizeImagesResult is the outcome of image recognition backfill.
type RecognizeImagesResult struct {
	Payload       []byte
	Applied       bool
	FallbackModel string // recognition model name for usage logging
}

// RecognizeCurrentTurnImages performs recognition backfill on current-turn images.
//   - Skips if analyzer is nil, no current-turn image, or current model supports native vision.
//   - Walks payload, calls analyzer.Analyze for each current-turn image,
//     replaces image part with summary text (or "[图片识别失败]" on error).
//   - Returns modified payload with Applied=true and FallbackModel=analyzer.Name().
func RecognizeCurrentTurnImages(ctx context.Context, analyzer ImageAnalyzer, payload []byte, currentModel string) RecognizeImagesResult {
	result := RecognizeImagesResult{Payload: payload}
	if analyzer == nil {
		return result
	}
	if !CurrentTurnHasImages(payload) {
		return result
	}
	if SupportsVisionByModelName(currentModel) {
		return result
	}

	walk := WalkPayload(payload)
	for _, ip := range walk.Parts {
		if !ip.IsCurrent {
			continue
		}
		placeholder := recognizeOneImage(ctx, analyzer, ip)
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
	return "[图片内容] " + text
}

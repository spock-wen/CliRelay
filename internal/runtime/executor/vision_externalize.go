package executor

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// externalizeImages prevents text-only models from receiving image content
// blocks, which upstream APIs reject with HTTP 400 ("unknown variant
// `image_url`, expected `text`"). That 400 fires at the model request layer,
// before any tool call — so it cannot be handled client-side (e.g. via MCP).
//
// Policy: vision-capable models (input modalities include "image") get the
// raw image blocks passed through unchanged — they read the image natively.
// Text-only models get every image block replaced with a fixed text
// placeholder, so the request still carries the turn's intent without the
// bytes that would 400. No server-side recognition is performed; if the
// client needs image understanding on a text model, that is the client's
// responsibility (e.g. a vision MCP tool it invokes itself).
//
// reqModel is the executor-facing model id, which is usually the resolved
// upstream id (e.g. "xopkimik26"). opts carries the client-requested alias
// ("kimi-k2.6") — both are consulted against the catalog, because modalities
// are keyed by whichever name the operator registered the model under.
func externalizeImages(cfg *config.Config, auth *cliproxyauth.Auth, reqModel string, opts cliproxyexecutor.Options, payload []byte) []byte {
	if cfg == nil || !cfg.Vision.Enabled {
		return payload
	}
	// Pure-text requests carry no images — skip the walk.
	if len(vision.WalkPayload(payload).Parts) == 0 {
		return payload
	}
	tenantID := ""
	if auth != nil {
		tenantID = auth.TenantID
	}
	if modelAcceptsImageInput(tenantID, reqModel, opts) {
		return payload
	}
	out, err := vision.ReplaceAllImages(payload, "[图片]")
	if err != nil {
		// A partial replacement can leave raw image bytes in the forwarded
		// payload — surface it so the leak is not silent.
		log.Warnf("vision: image replacement failed (image bytes may leak upstream to text model %q): %v", reqModel, err)
	}
	return out
}

// externalizeImagesCheap replaces image blocks with a fixed placeholder without
// consulting model modalities. Used by CountTokens, where the goal is only to
// keep the payload translatable (image blocks break translation) — the exact
// pass-through/placeholder decision does not affect a token estimate.
func externalizeImagesCheap(cfg *config.Config, payload []byte) []byte {
	if cfg == nil || !cfg.Vision.Enabled {
		return payload
	}
	out, err := vision.ReplaceAllImages(payload, "[图片]")
	if err != nil {
		log.Warnf("vision: cheap image replacement failed (image bytes may leak upstream): %v", err)
	}
	return out
}

// modelAcceptsImageInput reports whether the configured model catalog marks
// the request's model as accepting image input. It checks both the resolved
// upstream id (reqModel) and the client-requested alias (from opts), because
// modalities are keyed by whichever name the model was registered under. When
// neither is known to the catalog, the model is treated as text-only (the
// safe default: replace the image, never leak bytes that would 400).
func modelAcceptsImageInput(tenantID, reqModel string, opts cliproxyexecutor.Options) bool {
	candidates := []string{reqModel, payloadRequestedModel(opts, "")}
	for _, m := range candidates {
		if m == "" {
			continue
		}
		if modelCatalogAcceptsImage(tenantID, m) {
			return true
		}
	}
	return false
}

// modelCatalogAcceptsImage is the single catalog lookup: true when the model
// row exists and its input modalities include "image".
func modelCatalogAcceptsImage(tenantID, model string) bool {
	row, ok := usage.GetModelConfigForTenant(tenantID, model)
	if !ok {
		return false
	}
	for _, m := range row.InputModalities {
		if m == "image" {
			return true
		}
	}
	return false
}

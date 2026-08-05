package executor

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// externalizeImages replaces all image content blocks with text summaries so
// the upstream model never receives raw image bytes. Disabled vision is a
// strict no-op; enabled vision with no recognizer degrades to placeholders.
//
// It must run in every executor (Claude and OpenAI-compat) before payload
// translation, because real clients route through whichever channel the
// conductor selects — image blocks in any source format must never reach the
// chat model.
func externalizeImages(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, opts cliproxyexecutor.Options, payload []byte) []byte {
	if cfg == nil || !cfg.Vision.Enabled {
		return payload
	}
	// Pure-text requests carry no images — skip recognizer and payload walks.
	if len(vision.WalkPayload(payload).Parts) == 0 {
		return payload
	}
	analyzer := sharedVisionRecognizer(cfg)
	sessionKey, _ := vision.ResolveIsolatedSessionKey(opts, auth)

	if analyzer == nil {
		log.Warnf("vision: enabled but no recognizer available (channel %q) — replacing images with placeholders", cfg.Vision.Channel)
		var err error
		payload, err = vision.ReplaceAllImages(payload, "[Image Registry] 无可用的图片分析模型。")
		if err != nil {
			// A partial replacement can leave raw image bytes in the forwarded
			// payload — surface it so the leak is not silent.
			log.Warnf("vision: placeholder replacement failed (image bytes may leak upstream): %v", err)
		}
		return payload
	}

	proc := vision.NewProcessor(analyzer)
	procRes, _ := proc.Process(ctx, payload, sessionKey, 0)
	payload = procRes.Payload

	if vision.CurrentTurnHasImages(payload) {
		// A3ProcessCurrentTurn never returns a non-nil error — analysis
		// failures degrade to placeholders internally.
		payload, _ = proc.A3ProcessCurrentTurn(ctx, payload, sessionKey, 0)
	}
	return payload
}

// externalizeImagesCheap replaces image blocks with a fixed placeholder WITHOUT
// any analyzer round-trip. Used by CountTokens so token counting never triggers
// a kimi recognition call (and a new image is not recognized twice per turn).
func externalizeImagesCheap(cfg *config.Config, payload []byte) []byte {
	if cfg == nil || !cfg.Vision.Enabled {
		return payload
	}
	out, err := vision.ReplaceAllImages(payload, "[Image Registry] 图片（占位）")
	if err != nil {
		log.Warnf("vision: cheap placeholder replacement failed (image bytes may leak upstream): %v", err)
	}
	return out
}

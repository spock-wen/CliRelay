package executor

import (
	"context"
	"strings"
	"sync"
	"time"

	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

// cursorModelsCacheTTL bounds how long a fetched catalog is reused.
const cursorModelsCacheTTL = 10 * time.Minute

var cursorModelsCache struct {
	mu        sync.Mutex
	models    []*sdkmodelcatalog.ModelInfo
	fetchedAt time.Time
}

// FetchCursorModels retrieves the account's live model catalog via
// AgentService.GetUsableModels, falling back to the cached copy and finally to
// the static seed.
func FetchCursorModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*sdkmodelcatalog.ModelInfo {
	cursorModelsCache.mu.Lock()
	if len(cursorModelsCache.models) > 0 && time.Since(cursorModelsCache.fetchedAt) < cursorModelsCacheTTL {
		cached := cursorModelsCache.models
		cursorModelsCache.mu.Unlock()
		return cloneCursorModels(cached)
	}
	cursorModelsCache.mu.Unlock()

	token, baseURL := cursorCreds(auth)
	if token == "" {
		return fallbackCursorModels()
	}
	transport := newCursorTransport(cfg, auth)
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	live, err := transport.FetchUsableModels(fetchCtx, baseURL, token)
	if err != nil || len(live) == 0 {
		return fallbackCursorModels()
	}

	now := time.Now().Unix()
	models := make([]*sdkmodelcatalog.ModelInfo, 0, len(live))
	seen := make(map[string]struct{}, len(live))
	for _, item := range live {
		id := strings.TrimSpace(item.ModelID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		info := &sdkmodelcatalog.ModelInfo{
			ID:                  id,
			Object:              "model",
			Created:             now,
			OwnedBy:             "cursor",
			Type:                "cursor",
			Name:                firstNonEmptyString(item.DisplayName, id),
			DisplayName:         firstNonEmptyString(item.DisplayName, id),
			Description:         "Cursor subscription model. Availability may be region-gated per account.",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		}
		models = append(models, info)
	}

	cursorModelsCache.mu.Lock()
	cursorModelsCache.models = models
	cursorModelsCache.fetchedAt = time.Now()
	cursorModelsCache.mu.Unlock()
	return cloneCursorModels(models)
}

func cloneCursorModels(models []*sdkmodelcatalog.ModelInfo) []*sdkmodelcatalog.ModelInfo {
	if models == nil {
		return nil
	}
	out := make([]*sdkmodelcatalog.ModelInfo, len(models))
	for i := range models {
		clone := *models[i]
		out[i] = &clone
	}
	return out
}

func fallbackCursorModels() []*sdkmodelcatalog.ModelInfo {
	return []*sdkmodelcatalog.ModelInfo{
		{ID: "composer-2.5", Object: "model", OwnedBy: "cursor", Type: "cursor", Name: "composer-2.5", DisplayName: "Composer 2.5", ContextLength: 200000, MaxCompletionTokens: 64000},
		{ID: "composer-2.5-fast", Object: "model", OwnedBy: "cursor", Type: "cursor", Name: "composer-2.5-fast", DisplayName: "Composer 2.5 Fast", ContextLength: 200000, MaxCompletionTokens: 64000},
		{ID: "grok-4.6", Object: "model", OwnedBy: "cursor", Type: "cursor", Name: "grok-4.6", DisplayName: "Grok 4.6", ContextLength: 256000, MaxCompletionTokens: 64000},
		{ID: "grok-4.5", Object: "model", OwnedBy: "cursor", Type: "cursor", Name: "grok-4.5", DisplayName: "Grok 4.5", ContextLength: 256000, MaxCompletionTokens: 64000},
		{ID: "auto", Object: "model", OwnedBy: "cursor", Type: "cursor", Name: "auto", DisplayName: "Auto", ContextLength: 200000, MaxCompletionTokens: 64000},
	}
}

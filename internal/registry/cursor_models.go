package registry

// GetCursorModels returns the static seed of Cursor subscription models.
// The full catalog is fetched at runtime via AgentService.GetUsableModels;
// these entries guarantee routing works before the first dynamic fetch and
// document which families are available in which regions (cursor-native
// models like Composer/Grok work everywhere; Anthropic/OpenAI models are
// region-gated per account).
func GetCursorModels() []*ModelInfo {
	return []*ModelInfo{
		{
			ID:                  "composer-2.5",
			Object:              "model",
			Created:             1783526400,
			OwnedBy:             "cursor",
			Type:                "cursor",
			DisplayName:         "Composer 2.5",
			Name:                "composer-2.5",
			Description:         "Cursor's Composer 2.5 coding model. Available to all subscription regions.",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{ZeroAllowed: true, Levels: []string{"minimal", "low", "medium", "high"}},
		},
		{
			ID:                  "composer-2.5-fast",
			Object:              "model",
			Created:             1783526400,
			OwnedBy:             "cursor",
			Type:                "cursor",
			DisplayName:         "Composer 2.5 Fast",
			Name:                "composer-2.5-fast",
			Description:         "Faster Composer 2.5 variant.",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{ZeroAllowed: true, Levels: []string{"minimal", "low", "medium", "high"}},
		},
		{
			ID:                  "grok-4.6",
			Object:              "model",
			Created:             1785369600,
			OwnedBy:             "cursor",
			Type:                "cursor",
			DisplayName:         "Grok 4.6",
			Name:                "grok-4.6",
			Description:         "Grok 4.6 jointly trained by Cursor and SpaceXAI; available to subscription accounts in all regions.",
			ContextLength:       256000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{ZeroAllowed: true, Levels: []string{"minimal", "low", "medium", "high"}},
		},
		{
			ID:                  "grok-4.5",
			Object:              "model",
			Created:             1783526400,
			OwnedBy:             "cursor",
			Type:                "cursor",
			DisplayName:         "Grok 4.5",
			Name:                "grok-4.5",
			Description:         "Grok 4.5 jointly trained by Cursor and SpaceXAI.",
			ContextLength:       256000,
			MaxCompletionTokens: 64000,
			Thinking:            &ThinkingSupport{ZeroAllowed: true, Levels: []string{"minimal", "low", "medium", "high"}},
		},
		{
			ID:                  "auto",
			Object:              "model",
			Created:             1783526400,
			OwnedBy:             "cursor",
			Type:                "cursor",
			DisplayName:         "Auto",
			Name:                "auto",
			Description:         "Cursor's automatic model selection for coding tasks.",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
		},
	}
}

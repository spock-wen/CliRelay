package usage

import "testing"

func TestReplaceAllCcSwitchImportConfigsPersistsModelMappings(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	usageDBMu.Lock()
	db := usageDB
	usageDBMu.Unlock()
	if db == nil {
		t.Fatal("expected test db")
	}
	initCcSwitchImportConfigsTable(db)

	err := ReplaceAllCcSwitchImportConfigs([]CcSwitchImportConfigRow{
		{
			ID:                   "cfg-claude",
			ClientType:           "claude",
			ProviderName:         "Relay Claude",
			DefaultModel:         "kimi-k2.5",
			AllowedChannelGroups: []string{"kimicode"},
			EndpointPath:         "/v1",
			UsageAutoInterval:    30,
			APIKeyField:          "ANTHROPIC_API_KEY",
			ModelMappings: []CcSwitchModelMappingRow{
				{Role: "main", RequestModel: "kimi-k2.5", TargetModel: "kimi-k2.5"},
				{Role: "haiku", RequestModel: "claude-3-5-haiku", TargetModel: "kimi-k2.5"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceAllCcSwitchImportConfigs() error = %v", err)
	}

	rows := ListCcSwitchImportConfigs()
	if len(rows) != 1 {
		t.Fatalf("ListCcSwitchImportConfigs() length = %d, want 1: %#v", len(rows), rows)
	}
	if len(rows[0].ModelMappings) != 2 {
		t.Fatalf("model mappings length = %d, want 2: %#v", len(rows[0].ModelMappings), rows[0].ModelMappings)
	}
	if rows[0].ModelMappings[1].Role != "haiku" ||
		rows[0].ModelMappings[1].RequestModel != "claude-3-5-haiku" ||
		rows[0].ModelMappings[1].TargetModel != "kimi-k2.5" {
		t.Fatalf("model mapping not preserved: %#v", rows[0].ModelMappings[1])
	}
}

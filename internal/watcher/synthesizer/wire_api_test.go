package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestConfigSynthesizerCarriesWireAPI(t *testing.T) {
	auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{
				{Name: "GPT", BaseURL: "http://127.0.0.1:8317/v1", WireAPI: "responses", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "gpt-key"}}},
				{Name: "GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "glm-key"}}},
				{Name: "Keyless", BaseURL: "http://127.0.0.1:8317/v1", WireAPI: "responses"},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"gpt/gpt-key": true, "glm/glm-key": false, "keyless/": true}
	seen := map[string]bool{}
	for _, auth := range auths {
		key := auth.Provider + "/" + auth.Attributes["api_key"]
		expect, ok := want[key]
		if !ok {
			t.Fatalf("unexpected auth %s", key)
		}
		seen[key] = true
		got := auth.Attributes["wire_api"]
		if expect && got != config.OpenAICompatWireAPIResponses {
			t.Errorf("%s: wire_api = %q", key, got)
		}
		if !expect && got != "" {
			t.Errorf("%s: wire_api = %q, want absent", key, got)
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("missing auth %s", key)
		}
	}
}

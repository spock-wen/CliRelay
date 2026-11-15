package providers

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestPatchOpenAICompatibilityWireAPI(t *testing.T) {
	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:    "GPT",
		BaseURL: "http://127.0.0.1:8317/v1",
	}}}
	svc := NewService(cfg, nil)
	responses := " responses "
	if err := svc.PatchOpenAICompatibility(nil, &cfg.OpenAICompatibility[0].Name, OpenAICompatibilityPatch{WireAPI: &responses}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.OpenAICompatibility[0].WireAPI; got != config.OpenAICompatWireAPIResponses {
		t.Fatalf("wire-api = %q", got)
	}
	cleared := "chat"
	if err := svc.PatchOpenAICompatibility(nil, &cfg.OpenAICompatibility[0].Name, OpenAICompatibilityPatch{WireAPI: &cleared}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.OpenAICompatibility[0].WireAPI; got != "" {
		t.Fatalf("cleared wire-api = %q", got)
	}
	prefix := "team"
	cfg.OpenAICompatibility[0].WireAPI = config.OpenAICompatWireAPIResponses
	if err := svc.PatchOpenAICompatibility(nil, &cfg.OpenAICompatibility[0].Name, OpenAICompatibilityPatch{Prefix: &prefix}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.OpenAICompatibility[0].WireAPI; got != config.OpenAICompatWireAPIResponses {
		t.Fatalf("unrelated patch cleared wire-api: %q", got)
	}
}

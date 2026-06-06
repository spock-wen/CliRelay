package providers

import (
	"errors"
	"reflect"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestReplaceOpenAICompatibilityNormalizesFiltersAndRollsBack(t *testing.T) {
	validationErr := errors.New("duplicate channel")
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "existing",
			BaseURL: "https://old.example",
		}},
	}

	err := NewService(cfg, func() error { return validationErr }).ReplaceOpenAICompatibility([]config.OpenAICompatibility{{
		Name:    " next ",
		BaseURL: " https://next.example ",
	}})
	if !errors.Is(err, validationErr) {
		t.Fatalf("ReplaceOpenAICompatibility() error = %v, want validation error", err)
	}
	if got := cfg.OpenAICompatibility; len(got) != 1 || got[0].Name != "existing" {
		t.Fatalf("OpenAICompatibility after rollback = %#v, want existing entry", got)
	}

	err = NewService(cfg, nil).ReplaceOpenAICompatibility([]config.OpenAICompatibility{
		{
			Name:    " next ",
			BaseURL: " https://next.example ",
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{{
				APIKey:   " key-1 ",
				ProxyURL: " http://proxy.example ",
				ProxyID:  " proxy-a ",
			}},
			Headers: map[string]string{" x-trace ": " on "},
		},
		{
			Name:    "blank",
			BaseURL: " ",
		},
	})
	if err != nil {
		t.Fatalf("ReplaceOpenAICompatibility() error = %v, want nil", err)
	}
	got := cfg.OpenAICompatibility
	if len(got) != 1 {
		t.Fatalf("OpenAICompatibility len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Name != "next" || got[0].BaseURL != "https://next.example" {
		t.Fatalf("normalized entry = %#v, want trimmed values", got[0])
	}
	if got[0].APIKeyEntries[0].APIKey != "key-1" || got[0].APIKeyEntries[0].ProxyURL != "http://proxy.example" || got[0].APIKeyEntries[0].ProxyID != "proxy-a" {
		t.Fatalf("normalized api key entry = %#v", got[0].APIKeyEntries[0])
	}
	if _, ok := got[0].Headers["x-trace"]; !ok {
		t.Fatalf("headers = %#v, want normalized x-trace header", got[0].Headers)
	}
}

func TestPatchOpenAICompatibilityUpdatesAndDeletes(t *testing.T) {
	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:    "compat",
		BaseURL: "https://old.example",
	}}}
	name := " compat "
	newName := " renamed "
	disabled := true
	baseURL := " https://new.example "
	models := []config.OpenAICompatibilityModel{{Name: " gpt-4.1 ", Alias: " smart "}}

	err := NewService(cfg, nil).PatchOpenAICompatibility(nil, &name, OpenAICompatibilityPatch{
		Name:     &newName,
		Disabled: &disabled,
		BaseURL:  &baseURL,
		Models:   &models,
	})
	if err != nil {
		t.Fatalf("PatchOpenAICompatibility() error = %v, want nil", err)
	}
	if got := cfg.OpenAICompatibility[0]; got.Name != "renamed" || got.BaseURL != "https://new.example" || !got.Disabled {
		t.Fatalf("patched entry = %#v, want trimmed updated entry", got)
	}

	index := 0
	emptyBaseURL := " "
	err = NewService(cfg, nil).PatchOpenAICompatibility(&index, nil, OpenAICompatibilityPatch{BaseURL: &emptyBaseURL})
	if err != nil {
		t.Fatalf("PatchOpenAICompatibility(delete) error = %v, want nil", err)
	}
	if len(cfg.OpenAICompatibility) != 0 {
		t.Fatalf("OpenAICompatibility after delete = %#v, want empty", cfg.OpenAICompatibility)
	}
}

func TestVertexCompatKeysNormalizePatchAndDelete(t *testing.T) {
	cfg := &config.Config{}
	svc := NewService(cfg, nil)

	svc.ReplaceVertexCompatKeys([]config.VertexCompatKey{{
		APIKey:  " vertex-key ",
		BaseURL: " https://vertex.example ",
		Headers: map[string]string{
			" x-trace ": " on ",
		},
		Models: []config.VertexCompatModel{
			{Name: " gemini-pro ", Alias: " pro "},
			{Name: " ", Alias: "drop"},
		},
	}})

	if len(cfg.VertexCompatAPIKey) != 1 {
		t.Fatalf("VertexCompatAPIKey len = %d, want 1", len(cfg.VertexCompatAPIKey))
	}
	got := cfg.VertexCompatAPIKey[0]
	if got.APIKey != "vertex-key" || got.BaseURL != "https://vertex.example" {
		t.Fatalf("normalized vertex entry = %#v", got)
	}
	if !reflect.DeepEqual(got.Models, []config.VertexCompatModel{{Name: "gemini-pro", Alias: "pro"}}) {
		t.Fatalf("normalized models = %#v, want one trimmed model", got.Models)
	}

	match := " vertex-key "
	proxyURL := " http://proxy.example "
	err := svc.PatchVertexCompatKey(nil, &match, VertexCompatPatch{ProxyURL: &proxyURL})
	if err != nil {
		t.Fatalf("PatchVertexCompatKey() error = %v, want nil", err)
	}
	if cfg.VertexCompatAPIKey[0].ProxyURL != "http://proxy.example" {
		t.Fatalf("ProxyURL = %q, want trimmed proxy URL", cfg.VertexCompatAPIKey[0].ProxyURL)
	}

	index := 0
	emptyAPIKey := " "
	err = svc.PatchVertexCompatKey(&index, nil, VertexCompatPatch{APIKey: &emptyAPIKey})
	if err != nil {
		t.Fatalf("PatchVertexCompatKey(delete) error = %v, want nil", err)
	}
	if len(cfg.VertexCompatAPIKey) != 0 {
		t.Fatalf("VertexCompatAPIKey after delete = %#v, want empty", cfg.VertexCompatAPIKey)
	}
}

func TestGeminiKeysReplacePatchDeleteAndRollback(t *testing.T) {
	validationErr := errors.New("invalid channel")
	cfg := &config.Config{
		GeminiKey: []config.GeminiKey{{APIKey: "existing"}},
	}
	svc := NewService(cfg, func() error { return validationErr })

	err := svc.ReplaceGeminiKeys([]config.GeminiKey{{APIKey: " next "}})
	if !errors.Is(err, validationErr) {
		t.Fatalf("ReplaceGeminiKeys() error = %v, want validation error", err)
	}
	if got := cfg.GeminiKey; len(got) != 1 || got[0].APIKey != "existing" {
		t.Fatalf("GeminiKey after rollback = %#v, want existing entry", got)
	}

	svc = NewService(cfg, nil)
	err = svc.ReplaceGeminiKeys([]config.GeminiKey{{APIKey: " next ", Prefix: " /team/ ", ProxyURL: " http://proxy.example "}})
	if err != nil {
		t.Fatalf("ReplaceGeminiKeys() error = %v, want nil", err)
	}
	if got := cfg.GeminiKey[0]; got.APIKey != "next" || got.Prefix != "team" || got.ProxyURL != "http://proxy.example" {
		t.Fatalf("normalized gemini key = %#v", got)
	}

	match := " next "
	emptyAPIKey := " "
	err = svc.PatchGeminiKey(nil, &match, GeminiKeyPatch{APIKey: &emptyAPIKey})
	if err != nil {
		t.Fatalf("PatchGeminiKey(delete) error = %v, want nil", err)
	}
	if len(cfg.GeminiKey) != 0 {
		t.Fatalf("GeminiKey after delete = %#v, want empty", cfg.GeminiKey)
	}
}

func TestClaudeKeysReplacePatchAndDelete(t *testing.T) {
	cfg := &config.Config{}
	svc := NewService(cfg, nil)

	err := svc.ReplaceClaudeKeys([]config.ClaudeKey{
		{Name: "oauth-row", APIKey: ""},
		{
			Name:    " claude ",
			APIKey:  " sk-claude ",
			BaseURL: " https://claude.example ",
			Models:  []config.ClaudeModel{{Name: " claude-sonnet-4 ", Alias: " sonnet "}},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceClaudeKeys() error = %v, want nil", err)
	}
	if len(cfg.ClaudeKey) != 1 {
		t.Fatalf("ClaudeKey len = %d, want 1", len(cfg.ClaudeKey))
	}
	if got := cfg.ClaudeKey[0]; got.Name != "claude" || got.APIKey != "sk-claude" || got.BaseURL != "https://claude.example" {
		t.Fatalf("normalized claude key = %#v", got)
	}

	match := "sk-claude"
	blankAPIKey := " "
	err = svc.PatchClaudeKey(nil, &match, ClaudeKeyPatch{APIKey: &blankAPIKey})
	if err != nil {
		t.Fatalf("PatchClaudeKey(blank api key) error = %v, want nil", err)
	}
	if len(cfg.ClaudeKey) != 0 {
		t.Fatalf("ClaudeKey after blank api key patch = %#v, want empty", cfg.ClaudeKey)
	}
}

func TestCodexKeysReplacePatchDeleteAndRollback(t *testing.T) {
	validationErr := errors.New("channel conflict")
	cfg := &config.Config{
		CodexKey: []config.CodexKey{{APIKey: "existing", BaseURL: "https://old.example"}},
	}
	svc := NewService(cfg, func() error { return validationErr })

	err := svc.ReplaceCodexKeys([]config.CodexKey{{APIKey: "next", BaseURL: "https://new.example"}})
	if !errors.Is(err, validationErr) {
		t.Fatalf("ReplaceCodexKeys() error = %v, want validation error", err)
	}
	if got := cfg.CodexKey; len(got) != 1 || got[0].APIKey != "existing" {
		t.Fatalf("CodexKey after rollback = %#v, want existing entry", got)
	}

	svc = NewService(cfg, nil)
	err = svc.ReplaceCodexKeys([]config.CodexKey{
		{APIKey: "next", BaseURL: " https://codex.example ", ProxyURL: " http://proxy.example "},
		{APIKey: "drop", BaseURL: " "},
	})
	if err != nil {
		t.Fatalf("ReplaceCodexKeys() error = %v, want nil", err)
	}
	if len(cfg.CodexKey) != 1 {
		t.Fatalf("CodexKey len = %d, want 1", len(cfg.CodexKey))
	}
	if got := cfg.CodexKey[0]; got.BaseURL != "https://codex.example" || got.ProxyURL != "http://proxy.example" {
		t.Fatalf("normalized codex key = %#v", got)
	}

	match := "next"
	emptyBaseURL := " "
	err = svc.PatchCodexKey(nil, &match, CodexKeyPatch{BaseURL: &emptyBaseURL})
	if err != nil {
		t.Fatalf("PatchCodexKey(delete) error = %v, want nil", err)
	}
	if len(cfg.CodexKey) != 0 {
		t.Fatalf("CodexKey after delete = %#v, want empty", cfg.CodexKey)
	}
}

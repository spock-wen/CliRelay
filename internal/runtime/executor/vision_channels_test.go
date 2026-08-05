package executor

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
)

func TestVisionChannelBaseURLCollectsKeysByChannelName(t *testing.T) {
	cfg := &config.Config{
		ClaudeKey: []config.ClaudeKey{
			{Name: "xunfei-199", BaseURL: "https://kimi.example.com", APIKey: "k1"},
			{Name: "xunfei-199", BaseURL: "https://kimi.example.com", APIKey: "k2"},
			{Name: "other", BaseURL: "https://other.example.com", APIKey: "k9"},
		},
	}
	baseURL, keys := visionChannelBaseURL(cfg, "xunfei-199")
	if baseURL != "https://kimi.example.com" {
		t.Fatalf("baseURL = %q", baseURL)
	}
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Fatalf("keys = %v, want [k1 k2]", keys)
	}
}

func TestVisionChannelBaseURLUnknownChannel(t *testing.T) {
	cfg := &config.Config{ClaudeKey: []config.ClaudeKey{{Name: "x", BaseURL: "u", APIKey: "k"}}}
	baseURL, keys := visionChannelBaseURL(cfg, "missing")
	if baseURL != "" || len(keys) != 0 {
		t.Fatalf("expected empty, got %q %v", baseURL, keys)
	}
}

func TestNewVisionRecognizerDisabled(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Enabled = false
	e := NewClaudeExecutor(&config.Config{Vision: v})
	if r := e.newVisionRecognizer(); r != nil {
		t.Fatal("expected nil recognizer when disabled")
	}
}

func TestNewVisionRecognizerMissingChannel(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Enabled = true
	v.Channel = "missing"
	e := NewClaudeExecutor(&config.Config{
		Vision:    v,
		ClaudeKey: []config.ClaudeKey{{Name: "x", BaseURL: "u", APIKey: "k"}},
	})
	if r := e.newVisionRecognizer(); r != nil {
		t.Fatal("expected nil recognizer when channel not found")
	}
}

func TestNewVisionRecognizerBuilds(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Enabled = true
	v.Channel = "xunfei-199"
	e := NewClaudeExecutor(&config.Config{
		Vision:    v,
		ClaudeKey: []config.ClaudeKey{{Name: "xunfei-199", BaseURL: "https://kimi.example.com", APIKey: "k1"}},
	})
	r := e.newVisionRecognizer()
	if r == nil {
		t.Fatal("expected non-nil recognizer")
	}
	var _ *vision.Recognizer = r // compile-time type check
}

func TestNewVisionRecognizerBuildsFromBaseURLAndKeys(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Enabled = true
	v.BaseURL = "https://kimi.example.com"
	v.APIKeys = []string{"k1", "k2", "k3"} // multiple keys, no channel at all
	e := NewClaudeExecutor(&config.Config{Vision: v})
	r := e.newVisionRecognizer()
	if r == nil {
		t.Fatal("expected non-nil recognizer from vision.base-url + api-keys without any channel")
	}
	var _ *vision.Recognizer = r
}

func TestNewVisionRecognizerBaseURLWithoutKeysFallsBack(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Enabled = true
	v.BaseURL = "https://kimi.example.com" // base-url set but no api-keys
	// channel "xunfei-199" (default) does not resolve: no ClaudeKey matches → nil
	e := NewClaudeExecutor(&config.Config{Vision: v})
	if r := e.newVisionRecognizer(); r != nil {
		t.Fatal("expected nil recognizer when api-keys empty and no channel resolves")
	}
}

package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

const testImagePayload = `{
  "model": "test-model",
  "messages": [{"role": "user", "content": [
    {"type": "text", "text": "描述这张图"},
    {"type": "image", "source": {"type": "base64", "media_type": "image/jpeg", "data": "AAABAAEAAQ=="}}
  ]}]
}`

func visionTestConfig(enable bool, baseURL string, keys []string) *config.Config {
	v := config.DefaultVisionConfig()
	v.Enabled = enable
	v.BaseURL = baseURL
	v.APIKeys = keys
	return &config.Config{Vision: v}
}

func TestSharedVisionRecognizerAcrossExecutors(t *testing.T) {
	cfg := visionTestConfig(true, "https://kimi.example.com", []string{"k1"})
	claude := NewClaudeExecutor(cfg)
	compat := NewOpenAICompatExecutor("test", cfg)

	claudeRec := claude.newVisionRecognizer()
	compatRec := compat.newVisionRecognizer()
	if claudeRec == nil {
		t.Fatal("expected ClaudeExecutor recognizer to build")
	}
	if compatRec == nil {
		t.Fatal("expected OpenAICompatExecutor recognizer to build")
	}
	if claudeRec != compatRec {
		t.Fatal("expected both executors to share the SAME process-wide recognizer instance (same key pool / limiter)")
	}
}

func TestSharedVisionRecognizerRebuildsOnDifferentConfig(t *testing.T) {
	cfgA := visionTestConfig(true, "https://a.example.com", []string{"k1"})
	cfgB := visionTestConfig(true, "https://b.example.com", []string{"k1"})
	recA := NewClaudeExecutor(cfgA).newVisionRecognizer()
	recB := NewClaudeExecutor(cfgB).newVisionRecognizer()
	if recA == nil || recB == nil {
		t.Fatalf("expected both recognizers to build, got %v / %v", recA != nil, recB != nil)
	}
	if recA == recB {
		t.Fatal("expected a different recognizer when the vision config changes")
	}
}

func TestOpenAICompatNewVisionRecognizerDisabled(t *testing.T) {
	e := NewOpenAICompatExecutor("test", visionTestConfig(false, "https://kimi.example.com", []string{"k1"}))
	if r := e.newVisionRecognizer(); r != nil {
		t.Fatal("expected nil recognizer when vision disabled")
	}
}

func TestExternalizeImagesDisabledPassthrough(t *testing.T) {
	cfg := visionTestConfig(false, "", nil)
	out := externalizeImages(context.Background(), cfg, nil, cliproxyexecutor.Options{}, []byte(testImagePayload))
	if string(out) != testImagePayload {
		t.Fatal("expected byte-identical payload when vision disabled")
	}
}

func TestExternalizeImagesNoImagesPassthrough(t *testing.T) {
	cfg := visionTestConfig(true, "", nil)
	payload := `{"model":"x","messages":[{"role":"user","content":"plain text"}]}`
	out := externalizeImages(context.Background(), cfg, nil, cliproxyexecutor.Options{}, []byte(payload))
	if string(out) != payload {
		t.Fatalf("expected plain-text payload unchanged, got %s", out)
	}
}

func TestExternalizeImagesNoRecognizerPlaceholder(t *testing.T) {
	// Enabled vision with no resolvable base-url/keys -> recognizer nil -> the
	// image is replaced with a placeholder WITHOUT any network call.
	cfg := visionTestConfig(true, "", nil)
	out := externalizeImages(context.Background(), cfg, nil, cliproxyexecutor.Options{}, []byte(testImagePayload))
	s := string(out)
	if len(s) == 0 {
		t.Fatal("expected non-empty payload")
	}
	if !strings.Contains(s, "[Image Registry] 无可用的图片分析模型。") {
		t.Fatalf("expected placeholder text in output, got %s", s)
	}
	if strings.Contains(s, "AAABAAEAAQ==") {
		t.Fatal("expected raw image data to be removed from payload")
	}
}

func TestExternalizeImagesCheapPlaceholder(t *testing.T) {
	cfg := visionTestConfig(true, "", nil)
	out := externalizeImagesCheap(cfg, []byte(testImagePayload))
	s := string(out)
	if !strings.Contains(s, "[Image Registry] 图片（占位）") {
		t.Fatalf("expected cheap placeholder in output, got %s", s)
	}
	if strings.Contains(s, "AAABAAEAAQ==") {
		t.Fatal("expected raw image data to be removed from payload")
	}
}

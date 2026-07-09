package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestResolveRecognitionTargetFromConfig(t *testing.T) {
	cfg := &config.Config{
		VisionRecognitionModel: "openai-official/gpt-4o",
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "openai-official",
				BaseURL: "http://example",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "sk-test"},
				},
			},
		},
	}
	e := &OpenAICompatExecutor{provider: "openai-official", cfg: cfg}
	a := e.resolveRecognitionAnalyzer()
	if a == nil {
		t.Fatal("expected non-nil analyzer")
	}
}

func TestExecuteVisionRecognitionEndToEnd(t *testing.T) {
	// 1. 视觉识图 mock 端点
	visionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: a red box\nOCR: hello"}}]}`))
	}))
	defer visionSrv.Close()

	// 2. 上游文本模型 mock 端点，记录收到的 payload
	var upstreamBody string
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstreamSrv.Close()

	cfg := &config.Config{
		VisionRecognitionModel: "vision-provider/gpt-4o",
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "vision-provider",
				BaseURL: visionSrv.URL,
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "sk-vision"},
				},
			},
			{
				Name:    "text-provider",
				BaseURL: upstreamSrv.URL,
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "sk-text"},
				},
			},
		},
	}

	e := &OpenAICompatExecutor{provider: "text-provider", cfg: cfg}
	auth := &cliproxyauth.Auth{
		Provider: "text-provider",
		Attributes: map[string]string{
			"base_url": upstreamSrv.URL,
			"api_key":  "sk-text",
		},
	}

	payload := []byte(`{
		"model": "deepseek-v4",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "看图"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
			]}
		]
	}`)
	req := cliproxyexecutor.Request{Model: "deepseek-v4", Payload: payload}
	resp, err := e.Execute(context.Background(), auth, req, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(upstreamBody, "a red box") {
		t.Errorf("upstream should receive summary, got: %s", upstreamBody)
	}
	if strings.Contains(upstreamBody, "image_url") {
		t.Errorf("upstream should not receive image_url, got: %s", upstreamBody)
	}
	if len(resp.Payload) == 0 {
		t.Error("expected non-empty response")
	}
}

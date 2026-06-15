package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

// deepSeekMultiTurnToolPayload reproduces the 2026-06-15 production failure:
// a DeepSeek model routed via an OpenAI-compatibility provider (DeepSeek官方 /
// 火山引擎), with a multi-turn conversation where one assistant turn is
// tool_use-only (no thinking block). The Claude→OpenAI translator only emits
// reasoning_content for assistant turns that carry a thinking block, so the
// tool_use-only turn ships upstream WITHOUT the field — and DeepSeek thinking
// mode rejects the whole request with 400 "reasoning_content ... must be passed
// back".
const deepSeekMultiTurnToolPayload = `{
	"model": "deepseek-v4-pro",
	"max_tokens": 1024,
	"stream": true,
	"thinking": {"type": "enabled", "budget_tokens": 4096},
	"messages": [
		{"role": "user", "content": [{"type": "text", "text": "do the task"}]},
		{"role": "assistant", "content": [
			{"type": "thinking", "thinking": "I should read the file first."},
			{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {"file_path": "README.md"}}
		]},
		{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": "# Project\nA relay."}]},
		{"role": "assistant", "content": [
			{"type": "tool_use", "id": "toolu_2", "name": "Read", "input": {"file_path": "docs/intro.md"}}
		]},
		{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_2", "content": "intro"}]}
	],
	"tools": [
		{"name": "Read", "description": "Read file", "input_schema": {"type": "object"}}
	]
}`

// everyAssistantHasReasoningContent asserts that every assistant message in an
// upstream OpenAI request body carries a reasoning_content field. DeepSeek
// thinking mode requires this field on ALL assistant turns in multi-turn
// history; a single missing field triggers a 400.
func everyAssistantHasReasoningContent(t *testing.T, body []byte) {
	t.Helper()
	messages := gjson.GetBytes(body, "messages").Array()
	if len(messages) == 0 {
		t.Fatalf("no messages in upstream body: %s", string(body))
	}
	for i, msg := range messages {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		if !msg.Get("reasoning_content").Exists() {
			t.Fatalf("assistant message at index %d is missing reasoning_content (DeepSeek thinking mode rejects this with 400):\n%s", i, string(body))
		}
	}
}

// TestOpenAICompatExecutorDeepSeekStreamBackfillsReasoningForToolTurns verifies
// that a DeepSeek model routed through a generic OpenAI-compatibility provider
// (NOT opencode-go) still has reasoning_content injected on every assistant
// message — including tool_use-only turns the translator leaves bare.
func TestOpenAICompatExecutorDeepSeekStreamBackfillsReasoningForToolTurns(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	// Generic compat provider key — mirrors a DeepSeek官方 / 火山引擎
	// openai-compatibility entry, which is NOT the opencode-go provider.
	executor := NewOpenAICompatExecutor("deepseek-official", &config.Config{})
	auth := &cliproxyauth.Auth{
		ID: "auth-deepseek-1",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
		},
	}

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4-pro",
		Payload: []byte(deepSeekMultiTurnToolPayload),
	}, cliproxyexecutor.Options{
		OriginalRequest: []byte(deepSeekMultiTurnToolPayload),
		SourceFormat:    sdktranslator.FromString("claude"),
		Stream:          true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	for range result.Chunks {
	}

	everyAssistantHasReasoningContent(t, gotBody)
}

// TestOpenAICompatExecutorDeepSeekNonStreamBackfillsReasoningForToolTurns is the
// non-streaming counterpart of the above.
func TestOpenAICompatExecutorDeepSeekNonStreamBackfillsReasoningForToolTurns(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","model":"deepseek-v4-pro","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("deepseek-official", &config.Config{})
	auth := &cliproxyauth.Auth{
		ID: "auth-deepseek-1",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
		},
	}

	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "deepseek-v4-pro",
		Payload: []byte(deepSeekMultiTurnToolPayload),
	}, cliproxyexecutor.Options{
		OriginalRequest: []byte(deepSeekMultiTurnToolPayload),
		SourceFormat:    sdktranslator.FromString("claude"),
		Stream:          false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	everyAssistantHasReasoningContent(t, gotBody)
}

package executor

import (
	"context"
	"os"
	"strings"
	"testing"

	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// TestLiveCursorE2E exercises the real Cursor backend using a token file at
// /tmp/e2e/token (extracted from a logged-in store). Skipped when absent so
// CI never depends on live credentials.
func TestLiveCursorE2E(t *testing.T) {
	raw, err := os.ReadFile("/tmp/e2e/token")
	if err != nil {
		t.Skip("live token not provisioned (/tmp/e2e/token)")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		t.Skip("empty token")
	}

	auth := &cliproxyauth.Auth{
		ID:       "cursor-e2e",
		Provider: "cursor",
		Metadata: map[string]any{"access_token": token, "base_url": cursorauth.APIBaseURL},
	}
	ctx := context.Background()

	// 1. Model catalog via unary RPC.
	transport := newCursorTransport(nil, auth)
	models, errModels := transport.FetchUsableModels(ctx, cursorauth.APIBaseURL, token)
	if errModels != nil {
		t.Fatalf("GetUsableModels: %v", errModels)
	}
	if len(models) < 10 {
		t.Fatalf("expected live catalog, got %d models", len(models))
	}
	t.Logf("live catalog: %d models (first: %s)", len(models), models[0].ModelID)

	// 2. Region-blocked models surface either a mapped error or an empty
	// response on this account; both must not hang.
	blocked := cliproxyexecutor.Request{
		Model:  "claude-4.5-sonnet",
		Format: "anthropic",
		Payload: []byte(`{"model":"claude-4.5-sonnet","max_tokens":16,` +
			`"messages":[{"role":"user","content":"Reply with exactly one word: pong"}]}`),
	}
	blockedResp, errBlocked := NewCursorExecutor(nil).Execute(ctx, auth, blocked, cliproxyexecutor.Options{})
	if errBlocked != nil {
		t.Logf("claude model rejected: %.200s", errBlocked)
	} else {
		t.Logf("claude model returned empty body (region-gated): %.200s", blockedResp.Payload)
	}

	// 3. Available model must answer (non-stream).
	avail := cliproxyexecutor.Request{
		Model:  "composer-2.5",
		Format: "anthropic",
		Payload: []byte(`{"model":"composer-2.5","max_tokens":1024,` +
			`"messages":[{"role":"user","content":"Reply with exactly one word: pong"}]}`),
	}
	resp, errExec := NewCursorExecutor(nil).Execute(ctx, auth, avail, cliproxyexecutor.Options{})
	if errExec != nil {
		t.Fatalf("Execute: %v", errExec)
	}
	body := string(resp.Payload)
	t.Logf("live response: %.300s", body)
	if !strings.Contains(strings.ToLower(body), "pong") {
		t.Errorf("response missing pong: %s", body)
	}

	// 4. Streaming must deliver Anthropic SSE events ending in message_stop.
	stream, errStream := NewCursorExecutor(nil).ExecuteStream(ctx, auth, avail, cliproxyexecutor.Options{})
	if errStream != nil {
		t.Fatalf("ExecuteStream: %v", errStream)
	}
	var sse strings.Builder
	sawStop := false
	for chunk := range stream.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		sse.Write(chunk.Payload)
		if strings.Contains(string(chunk.Payload), `"message_stop"`) {
			sawStop = true
		}
	}
	t.Logf("stream SSE sample: %.200s", sse.String())
	if !sawStop {
		t.Errorf("stream missing message_stop event")
	}
	if !strings.Contains(sse.String(), "text_delta") {
		t.Errorf("stream missing text_delta events")
	}
}

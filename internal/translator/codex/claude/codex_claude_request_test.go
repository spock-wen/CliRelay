package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertClaudeRequestToCodex_StripsDeferLoading(t *testing.T) {
	input := []byte(`{
		"model": "claude-sonnet-4-6",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"name": "WebSearch", "description": "search", "input_schema": {"type": "object"}, "defer_loading": true},
			{"name": "Read", "description": "read", "input_schema": {"type": "object"}}
		]
	}`)

	out := ConvertClaudeRequestToCodex("gpt-5.2", input, true)

	// Ensure defer_loading is removed from all tools in the output
	tools := gjson.GetBytes(out, "tools")
	if !tools.IsArray() {
		t.Fatal("expected tools to be an array")
	}
	tools.ForEach(func(i, tool gjson.Result) bool {
		if tool.Get("defer_loading").Exists() {
			t.Fatalf("tool %d still has defer_loading: %s", i.Int(), tool.Raw)
		}
		return true
	})
}

func TestConvertClaudeRequestToCodex_SystemRoleBecomesDeveloper(t *testing.T) {
	input := []byte(`{
		"model":"gpt-6.1-sol",
		"max_tokens":16,
		"system":[{"type":"text","text":"Be brief."}],
		"messages":[
			{"role":"user","content":"first"},
			{"role":"system","content":[{"type":"text","text":"Stay brief."}]},
			{"role":"user","content":"ping"}
		]
	}`)
	out := ConvertClaudeRequestToCodex("gpt-6.1-sol", input, true)
	inputItems := gjson.GetBytes(out, "input")
	if !inputItems.IsArray() || len(inputItems.Array()) != 4 {
		t.Fatalf("input items = %s", inputItems.Raw)
	}
	roles := make([]string, 0, 4)
	inputItems.ForEach(func(_, item gjson.Result) bool {
		roles = append(roles, item.Get("role").String())
		return true
	})
	if roles[0] != "developer" || roles[2] != "developer" {
		t.Fatalf("roles = %v, want developer at the system turns", roles)
	}
	for _, role := range roles {
		if role == "system" {
			t.Fatalf("system role leaked into codex input: %v", roles)
		}
	}
	if gjson.GetBytes(out, "input.2.content.0.text").String() != "Stay brief." {
		t.Fatalf("mid-conversation text = %s", out)
	}
}

func TestConvertClaudeRequestToCodex_EmptyToolsOmitsToolChoice(t *testing.T) {
	input := []byte(`{
		"model":"claude-haiku-4-5",
		"messages":[{"role":"user","content":"Create a title"}],
		"tools":[],
		"tool_choice":{"type":"auto"}
	}`)

	out := ConvertClaudeRequestToCodex("grok-4", input, false)
	if gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatalf("empty tools must not emit tool_choice: %s", out)
	}
	if tools := gjson.GetBytes(out, "tools"); tools.Exists() && len(tools.Array()) != 0 {
		t.Fatalf("unexpected translated tools: %s", out)
	}
}

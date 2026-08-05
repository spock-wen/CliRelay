package vision

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const toolResultPayload = `{
  "model": "claude-3",
  "messages": [
    {"role": "user", "content": [
      {"type": "text", "text": "what does the screenshot show"},
      {"type": "tool_result", "tool_use_id": "t1", "content": [
        {"type": "image", "source": {"type": "base64", "media_type": "image/jpeg", "data": "AAABAAEAAQ=="}}
      ]}
    ]}
  ]
}`

func TestWalkPayloadFindsImageInToolResultContent(t *testing.T) {
	walk := WalkPayload([]byte(toolResultPayload))
	if len(walk.Parts) != 1 {
		t.Fatalf("parts = %d, want 1 (image nested in tool_result)", len(walk.Parts))
	}
	p := walk.Parts[0]
	if p.Data != "AAABAAEAAQ==" {
		t.Fatalf("data = %q, want the base64 payload", p.Data)
	}
	if p.Path != "messages.0.content.1.content.0" {
		t.Fatalf("path = %q, want messages.0.content.1.content.0", p.Path)
	}
	if !p.IsCurrent {
		t.Fatal("tool_result image in the last user message must be current")
	}
	if !walk.CurrentImages {
		t.Fatal("CurrentImages should be true")
	}
}

func TestWalkPayloadFindsImageInToolRoleMessage(t *testing.T) {
	payload := `{
  "model": "gpt",
  "messages": [
    {"role": "user", "content": "check the tool"},
    {"role": "assistant", "tool_calls": [{"id": "t1", "type": "function", "function": {"name": "screenshot", "arguments": "{}"}}]},
    {"role": "tool", "tool_call_id": "t1", "content": [
      {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAABAAEAAQ=="}}
    ]}
  ]
}`
	walk := WalkPayload([]byte(payload))
	if len(walk.Parts) != 1 {
		t.Fatalf("parts = %d, want 1 (image in role=tool message)", len(walk.Parts))
	}
	p := walk.Parts[0]
	if p.Data != "AAABAAEAAQ==" {
		t.Fatalf("data = %q, want the base64 payload", p.Data)
	}
	if p.MIMEType != "image/png" {
		t.Fatalf("mime = %q, want image/png", p.MIMEType)
	}
	if p.IsCurrent {
		t.Fatal("role=tool image must be historical (not the last user message)")
	}
	if walk.CurrentImages {
		t.Fatal("CurrentImages should be false for a historical-only tool image")
	}
}

func TestWalkPayloadFindsDataImageStringInToolResult(t *testing.T) {
	payload := `{
  "messages": [
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "t1", "content": "data:image/jpeg;base64,AAABAAEAAQ=="}
    ]}
  ]
}`
	walk := WalkPayload([]byte(payload))
	if len(walk.Parts) != 1 {
		t.Fatalf("parts = %d, want 1 (data:image string in tool_result content)", len(walk.Parts))
	}
	if walk.Parts[0].Path != "messages.0.content.0.content" {
		t.Fatalf("path = %q, want messages.0.content.0.content", walk.Parts[0].Path)
	}
}

func TestReplaceAllImagesReplacesToolResultImage(t *testing.T) {
	out, err := ReplaceAllImages([]byte(toolResultPayload), "[Image Registry] placeholder")
	if err != nil {
		t.Fatalf("ReplaceAllImages: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "AAABAAEAAQ==") {
		t.Fatal("raw base64 image still present after replacement")
	}
	// The nested image part must now be a text placeholder.
	got := gjson.GetBytes(out, "messages.0.content.1.content.0.type").String()
	if got != "text" {
		t.Fatalf("replaced part type = %q, want text", got)
	}
	if gjson.GetBytes(out, "messages.0.content.1.content.0.text").String() != "[Image Registry] placeholder" {
		t.Fatalf("replaced part text not set to placeholder: %s", out)
	}
	if gjson.GetBytes(out, "messages.0.content.1.content.0.source").Exists() {
		t.Fatal("image source block not removed")
	}
}

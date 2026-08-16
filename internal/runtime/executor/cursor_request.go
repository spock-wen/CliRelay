package executor

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor/cursorproto"
	"github.com/tidwall/gjson"
)

// cursorRequestShape is the Anthropic Messages payload the executor receives
// after upstream translation (TargetFormat = anthropic/claude).
type cursorRequestShape struct {
	System       string
	Messages     []cursorMessage
	Tools        []cursorTool
	Model        string
	MaxTokens    int64
	Conversation string
}

type cursorMessage struct {
	Role    string
	Content string
	Blocks  []cursorContentBlock
}

type cursorContentBlock struct {
	Type      string
	Text      string
	Thinking  string
	ID        string
	Name      string
	Input     json.RawMessage
	ToolUseID string
	Content   string
}

type cursorTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// parseCursorRequest extracts the conversation shape from an Anthropic
// Messages JSON payload.
func parseCursorRequest(payload []byte) (*cursorRequestShape, error) {
	shape := &cursorRequestShape{}
	result := gjson.ParseBytes(payload)
	shape.Model = result.Get("model").String()
	if v := result.Get("max_tokens"); v.Exists() {
		shape.MaxTokens = v.Int()
	}
	if v := result.Get("conversation_id"); v.Exists() {
		shape.Conversation = v.String()
	}
	if sys := result.Get("system"); sys.Exists() {
		if sys.IsArray() {
			var parts []string
			for _, item := range sys.Array() {
				parts = append(parts, item.Get("text").String())
			}
			shape.System = strings.Join(parts, "\n\n")
		} else {
			shape.System = sys.String()
		}
	}
	for _, tool := range result.Get("tools").Array() {
		schema := tool.Get("input_schema").Raw
		if schema == "" {
			schema = `{"type":"object"}`
		}
		shape.Tools = append(shape.Tools, cursorTool{
			Name:        tool.Get("name").String(),
			Description: tool.Get("description").String(),
			InputSchema: json.RawMessage(schema),
		})
	}
	for _, msg := range result.Get("messages").Array() {
		m := cursorMessage{Role: msg.Get("role").String()}
		content := msg.Get("content")
		if content.Type == gjson.String {
			m.Content = content.String()
		} else {
			for _, block := range content.Array() {
				b := cursorContentBlock{
					Type: block.Get("type").String(),
					Text: block.Get("text").String(),
					ID:   block.Get("id").String(),
					Name: block.Get("name").String(),
				}
				if raw := block.Get("input").Raw; raw != "" {
					b.Input = json.RawMessage(raw)
				}
				if raw := block.Get("content").Raw; raw != "" && block.Get("content").Type == gjson.String {
					b.Content = block.Get("content").String()
				} else if block.Get("content").IsArray() {
					var parts []string
					for _, sub := range block.Get("content").Array() {
						parts = append(parts, sub.Get("text").String())
					}
					b.Content = strings.Join(parts, "\n")
				}
				if raw := block.Get("thinking").String(); raw != "" {
					b.Thinking = raw
				}
				b.ToolUseID = block.Get("tool_use_id").String()
				m.Blocks = append(m.Blocks, b)
			}
		}
		shape.Messages = append(shape.Messages, m)
	}
	return shape, nil
}

// buildCursorClientMessage converts the Anthropic request into an
// AgentClientMessage for the Run RPC.
//
// Mapping:
//   - system prompt    -> custom_system_prompt
//   - message history  -> conversation_state.turns (ConversationTurnStructure
//     blobs: each user message starts a turn; following assistant/thinking
//     messages become that turn's steps)
//   - last user msg    -> action.user_message_action.user_message
//   - tools            -> mcp_tools (function-style tool definitions)
//   - model            -> requested_model.model_id
func buildCursorClientMessage(shape *cursorRequestShape, conversationID string, blobStore *cursorSession) *cursorproto.AgentClientMessage {
	var lastUserText string
	for _, msg := range shape.Messages {
		if msg.Role == "user" {
			text := msg.Content
			if text == "" {
				text = renderUserBlocks(msg.Blocks)
			}
			lastUserText = text
		}
	}

	action := &cursorproto.ConversationAction{
		UserMessageAction: &cursorproto.UserMessageAction{
			UserMessage: &cursorproto.UserMessage{
				Text:      lastUserText,
				MessageID: uuid.NewString(),
			},
		},
	}
	if lastUserText == "" {
		// No user message (e.g. continuation): resume instead.
		action.UserMessageAction = nil
		action.ResumeAction = &cursorproto.ResumeAction{}
	}

	// Prompt history travels as content-addressed blobs: each root entry is
	// the 32-byte SHA-256 of its JSON envelope, and the blob store maps
	// hex(hash) -> envelope bytes so server getBlob requests resolve.
	var rootMessages [][]byte
	addRoot := func(role, text string) {
		if text == "" {
			return
		}
		envelope := (&cursorproto.PromptMessage{Role: role, Content: text}).EncodeJSON()
		sum := sha256.Sum256(envelope)
		rootMessages = append(rootMessages, sum[:])
		blobStore.Put(sum[:], envelope)
	}
	if shape.System != "" {
		addRoot("system", shape.System)
	}
	for _, msg := range shape.Messages {
		switch msg.Role {
		case "user":
			text := msg.Content
			if text == "" {
				text = renderUserBlocks(msg.Blocks)
			}
			addRoot("user", text)
		case "assistant":
			text := msg.Content
			if text == "" {
				text, _ = renderAssistantBlocksSplit(msg.Blocks)
			}
			addRoot("assistant", text)
		}
	}
	runReq := &cursorproto.AgentRunRequest{
		ConversationState: &cursorproto.ConversationStateStructure{
			RootPromptMessages: rootMessages,
		},
		Action:         action,
		RequestedModel: &cursorproto.RequestedModel{ModelID: shape.Model},
		ModelDetails:   &cursorproto.ModelDetails{ModelID: shape.Model, DisplayModelID: shape.Model, DisplayName: shape.Model},
		ConversationID: conversationID,
	}
	if len(shape.Tools) > 0 {
		defs := make([]*cursorproto.McpToolDefinition, 0, len(shape.Tools))
		for _, tool := range shape.Tools {
			schema := compactJSON(string(tool.InputSchema))
			if schema == "" {
				schema = `{"type":"object"}`
			}
			defs = append(defs, &cursorproto.McpToolDefinition{
				Name:            tool.Name,
				ToolName:        tool.Name,
				Description:     tool.Description,
				InputSchemaJSON: schema,
			})
		}
		runReq.McpTools = &cursorproto.McpTools{Tools: defs}
	}
	return &cursorproto.AgentClientMessage{RunRequest: runReq}
}

// renderAssistantBlocksSplit renders assistant blocks into (text, thinking).
func renderAssistantBlocksSplit(blocks []cursorContentBlock) (string, string) {
	var textParts, thinkingParts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "thinking":
			if b.Thinking != "" {
				thinkingParts = append(thinkingParts, b.Thinking)
			}
		case "tool_use":
			// Neutral prose only: bracketed tool syntax teaches the model
			// to forge tool calls as text instead of real tool_call frames.
			textParts = append(textParts, "Calling tool "+b.Name+".")
		}
	}
	return strings.Join(textParts, "\n\n"), strings.Join(thinkingParts, "\n\n")
}

func renderUserBlocks(blocks []cursorContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "tool_result":
			content := b.Content
			if content == "" {
				content = "(empty tool result)"
			}
			// Plain prose, deliberately: bracketed tool_use/tool_result
			// syntax in history teaches the model to forge tool calls as
			// text instead of emitting real tool_call frames.
			parts = append(parts, "Tool output:\n"+content)
		case "image":
			parts = append(parts, "[image omitted]")
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderAssistantBlocks(blocks []cursorContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "thinking":
			if b.Thinking != "" {
				parts = append(parts, "[thinking] "+b.Thinking)
			}
		case "tool_use":
			// Omitted: rendering the call here teaches the model to mimic
			// tool calls as text. The paired tool_result carries the outcome.
			continue
		}
	}
	return strings.Join(parts, "\n\n")
}

func compactJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(raw)); err != nil {
		return raw
	}
	return buf.String()
}

// cursorShellToolName resolves the client-side tool that should execute
// native Cursor shell calls: a registered tool named "Bash" (case-insensitive),
// else the first tool whose schema declares a "command" property, else "".
func cursorShellToolName(tools []cursorTool) string {
	for _, t := range tools {
		if strings.EqualFold(t.Name, "bash") {
			return t.Name
		}
	}
	for _, t := range tools {
		if strings.Contains(string(t.InputSchema), `"command"`) {
			return t.Name
		}
	}
	return ""
}

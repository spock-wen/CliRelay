package executor

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor/cursorproto"
)

// cursorAnthropicAssembler converts AgentServerMessage frames into Anthropic
// Messages SSE events and a final non-stream JSON body.
//
// Block lifecycle mirrors Anthropic semantics:
//   message_start -> [content_block_start i; deltas i; content_block_stop i]* -> message_delta -> message_stop
type cursorAnthropicAssembler struct {
	Model     string
	MessageID string

	started     bool
	closed      bool
	blockIndex  int64
	openKind   string // "", "text", "thinking", "tool_use"
	toolCalls   []cursorToolAccumulator
	textBuf     []byte
	thinkingBuf []byte
	outputTokens int64
	inputTokens  int64
}

type cursorToolAccumulator struct {
	CallID  string
	Name    string
	Input   []byte
}

func newCursorAnthropicAssembler(model string) *cursorAnthropicAssembler {
	return &cursorAnthropicAssembler{
		Model:     model,
		MessageID: "msg_" + uuid.NewString(),
	}
}

// cursorEvent is one synthesized Anthropic SSE payload.
type cursorEvent struct {
	SSE map[string]any
}

func (a *cursorAnthropicAssembler) emit() []cursorEvent { return nil }

// ensureStarted opens the message on first content.
func (a *cursorAnthropicAssembler) ensureStarted(events []cursorEvent) []cursorEvent {
	if a.started {
		return events
	}
	a.started = true
	return append([]cursorEvent{{SSE: map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      a.MessageID,
			"type":    "message",
			"role":    "assistant",
			"model":   a.Model,
			"content": []any{},
			"usage":   map[string]any{"input_tokens": a.inputTokens, "output_tokens": 0},
		},
	}}}, events...)
}

// openBlock emits content_block_start for the wanted kind, closing any open block first.
func (a *cursorAnthropicAssembler) openBlock(kind string, callID string, name string, input json.RawMessage) []cursorEvent {
	var events []cursorEvent
	if a.openKind != "" {
		events = append(events, a.closeOpenBlock()...)
	}
	block := map[string]any{}
	switch kind {
	case "text":
		block = map[string]any{"type": "text", "text": ""}
	case "thinking":
		block = map[string]any{"type": "thinking", "thinking": ""}
	case "tool_use":
		block = map[string]any{"type": "tool_use", "id": callID, "name": name, "input": input}
	}
	events = append(events, cursorEvent{SSE: map[string]any{
		"type":          "content_block_start",
		"index":         a.blockIndex,
		"content_block": block,
	}})
	a.openKind = kind
	if kind == "tool_use" {
		a.openKind = "tool_use"
	}
	return events
}

func (a *cursorAnthropicAssembler) closeOpenBlock() []cursorEvent {
	if a.openKind == "" {
		return nil
	}
	ev := cursorEvent{SSE: map[string]any{
		"type":  "content_block_stop",
		"index": a.blockIndex,
	}}
	a.blockIndex++
	a.openKind = ""
	return []cursorEvent{ev}
}

// Handle folds one server message into Anthropic events.
func (a *cursorAnthropicAssembler) Handle(msg *cursorproto.AgentServerMessage) []cursorEvent {
	if msg == nil || msg.InteractionUpdate == nil {
		return nil
	}
	upd := msg.InteractionUpdate
	var events []cursorEvent
	switch {
	case upd.HasThinkingDelta:
		if a.openKind != "thinking" {
			events = append(events, a.openBlock("thinking", "", "", nil)...)
		}
		a.thinkingBuf = append(a.thinkingBuf, upd.ThinkingDelta...)
		events = append(events, cursorEvent{SSE: map[string]any{
			"type":  "content_block_delta",
			"index": a.blockIndex,
			"delta": map[string]any{"type": "thinking_delta", "thinking": upd.ThinkingDelta},
		}})
	case upd.HasTextDelta:
		if a.openKind != "text" {
			events = append(events, a.openBlock("text", "", "", nil)...)
		}
		a.textBuf = append(a.textBuf, upd.TextDelta...)
		events = append(events, cursorEvent{SSE: map[string]any{
			"type":  "content_block_delta",
			"index": a.blockIndex,
			"delta": map[string]any{"type": "text_delta", "text": upd.TextDelta},
		}})
	case upd.ToolCallStarted != nil:
		call := upd.ToolCallStarted
		name, input := cursorToolNameAndInput(call)
		events = append(events, a.openBlock("tool_use", call.CallID, name, input)...)
		a.toolCalls = append(a.toolCalls, cursorToolAccumulator{CallID: call.CallID, Name: name, Input: input})
	case upd.ToolCallDelta != nil:
		if upd.ToolCallDelta.ArgsPartial != "" {
			if idx := a.findTool(upd.ToolCallDelta.CallID); idx >= 0 {
				a.toolCalls[idx].Input = append(a.toolCalls[idx].Input, upd.ToolCallDelta.ArgsPartial...)
			}
		}
	case upd.ToolCallCompleted != nil:
		call := upd.ToolCallCompleted
		if name, input, ok := cursorCompletedToolInput(call); ok {
			if idx := a.findTool(call.CallID); idx >= 0 {
				a.toolCalls[idx].Name = name
				a.toolCalls[idx].Input = input
			} else {
				events = append(events, a.openBlock("tool_use", call.CallID, name, input)...)
				a.toolCalls = append(a.toolCalls, cursorToolAccumulator{CallID: call.CallID, Name: name, Input: input})
			}
		}
	case upd.HasTokenDelta:
		a.outputTokens += int64(upd.TokenDelta)
	case upd.TurnEnded:
		events = append(events, a.Finish("end_turn")...)
	}
	return a.ensureStarted(events)
}

// Finish closes open blocks and emits the terminal events.
func (a *cursorAnthropicAssembler) Finish(stop string) []cursorEvent {
	if a.closed {
		return nil
	}
	events := a.closeOpenBlock()
	if !a.started {
		events = append(events, cursorEvent{SSE: map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": a.MessageID, "type": "message", "role": "assistant", "model": a.Model,
				"content": []any{}, "usage": map[string]any{"input_tokens": a.inputTokens, "output_tokens": 0},
			},
		}})
		if len(a.textBuf) == 0 && len(a.toolCalls) == 0 {
			events = append(events, cursorEvent{SSE: map[string]any{
				"type": "content_block_start", "index": int64(0),
				"content_block": map[string]any{"type": "text", "text": ""},
			}})
			events = append(events, cursorEvent{SSE: map[string]any{
				"type": "content_block_stop", "index": int64(0),
			}})
		}
		a.started = true
	}
	events = append(events, cursorEvent{SSE: map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": a.outputTokens},
	}})
	events = append(events, cursorEvent{SSE: map[string]any{"type": "message_stop"}})
	a.closed = true
	return events
}

func (a *cursorAnthropicAssembler) findTool(callID string) int {
	for i := range a.toolCalls {
		if a.toolCalls[i].CallID == callID {
			return i
		}
	}
	return -1
}

// BuildNonStreamResponse assembles the full Anthropic Messages JSON body.
func (a *cursorAnthropicAssembler) BuildNonStreamResponse(stop string) []byte {
	content := []map[string]any{}
	if len(a.thinkingBuf) > 0 {
		content = append(content, map[string]any{"type": "thinking", "thinking": string(a.thinkingBuf)})
	}
	if len(a.textBuf) > 0 {
		content = append(content, map[string]any{"type": "text", "text": string(a.textBuf)})
	}
	for _, tc := range a.toolCalls {
		input := tc.Input
		if len(input) == 0 || !json.Valid(input) {
			input = []byte("{}")
		}
		content = append(content, map[string]any{
			"type": "tool_use", "id": tc.CallID, "name": tc.Name, "input": json.RawMessage(input),
		})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}
	body := map[string]any{
		"id": a.MessageID, "type": "message", "role": "assistant", "model": a.Model,
		"content": content, "stop_reason": stop, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": a.inputTokens, "output_tokens": a.outputTokens},
	}
	out, _ := json.Marshal(body)
	return out
}

// cursorToolNameAndInput extracts a best-effort name/input from a started
// tool call. Cursor native tools carry typed payloads; MCP calls carry args.
func cursorToolNameAndInput(call *cursorproto.ToolCallEvent) (string, json.RawMessage) {
	name, input, ok := cursorCompletedToolInput(call)
	if !ok {
		return "tool", json.RawMessage("{}")
	}
	return name, input
}

// cursorCompletedToolInput parses the ToolCall blob for MCP calls
// (mcp_tool_call = field 15 of ToolCall).
func cursorCompletedToolInput(call *cursorproto.ToolCallEvent) (string, json.RawMessage, bool) {
	if call == nil || len(call.ToolCallRaw) == 0 {
		return "", nil, false
	}
	name, input := cursorproto.ParseMcpToolCall(call.ToolCallRaw)
	if name == "" {
		return "", nil, false
	}
	if len(input) == 0 {
		input = []byte("{}")
	}
	return name, input, true
}

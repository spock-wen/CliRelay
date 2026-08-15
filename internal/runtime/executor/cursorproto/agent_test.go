package cursorproto

import (
	"encoding/binary"
	"os"
	"testing"
)

// The repo-root descriptor binary is the contract source (.cursor-agent-descriptor.bin).
const descriptorPath = "../../../../.cursor-agent-descriptor.bin"

func readVarintTB(b []byte) (uint64, []byte) {
	v, n := binary.Uvarint(b)
	if n <= 0 || n > len(b) {
		return 0, nil // malformed: caller stops on nil
	}
	return v, b[n:]
}


// loadDescriptorMessages parses the FileDescriptorProto into field tables.
func loadDescriptorMessages(t *testing.T) map[string]map[int]string {
	t.Helper()
	raw, err := os.ReadFile(descriptorPath)
	if err != nil {
		t.Skipf("descriptor not found (%v)", err)
	}
	msgs := make(map[string]map[int]string)
	b := raw
	for len(b) > 0 {
		key, rest, ok := nextField(b)
		if !ok {
			break
		}
		field, wire := key>>3, key&7
		if wire != wireBytes {
			if wire == 0 {
				_, b, ok = nextVarint(rest)
				if !ok {
					break
				}
				continue
			}
			break
		}
		payload, rest2, ok := nextBytes(rest)
		if !ok {
			break
		}
		b = rest2
		if field != 4 { // 4 = DescriptorProto
			continue
		}
		name, fields, ok := parseMessageDescriptor(payload)
		if ok && name != "" {
			msgs[name] = fields
		}
	}
	return msgs
}

func nextVarint(b []byte) (uint64, []byte, bool) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, nil, false
	}
	return v, b[n:], true
}

func nextField(b []byte) (uint64, []byte, bool) {
	v, rest, ok := nextVarint(b)
	return v, rest, ok
}

func nextBytes(b []byte) ([]byte, []byte, bool) {
	ln, rest, ok := nextVarint(b)
	if !ok || uint64(len(rest)) < ln {
		return nil, nil, false
	}
	return rest[:ln], rest[ln:], true
}

func parseMessageDescriptor(payload []byte) (string, map[int]string, bool) {
	var name string
	fields := make(map[int]string)
	mb := payload
	for len(mb) > 0 {
		key, rest, ok := nextField(mb)
		if !ok {
			return "", nil, false
		}
		mf, mw := int(key>>3), int(key&7)
		if mw != 2 {
			if mw == 0 {
				_, mb, ok = nextVarint(rest)
				if !ok {
					return "", nil, false
				}
				continue
			}
			return "", nil, false
		}
		val, mb2, ok := nextBytes(rest)
		if !ok {
			return "", nil, false
		}
		mb = mb2
		switch mf {
		case 1: // message name
			name = string(val)
		case 2: // field descriptor
			fname, fno, ok := parseFieldDescriptor(val)
			if !ok {
				continue
			}
			fields[fno] = fname
		}
	}
	return name, fields, true
}

func parseFieldDescriptor(val []byte) (string, int, bool) {
	var fname string
	fno := 0
	fb := val
	for len(fb) > 0 {
		key, rest, ok := nextField(fb)
		if !ok {
			return "", 0, false
		}
		ff, fw := int(key>>3), int(key&7)
		switch {
		case fw == 0: // varint
			v, rest2, ok2 := nextVarint(rest)
			if !ok2 {
				return "", 0, false
			}
			if ff == 3 { // number
				fno = int(v)
			}
			fb = rest2
		case fw == 2: // length-delimited
			fv, rest2, ok2 := nextBytes(rest)
			if !ok2 {
				return "", 0, false
			}
			if ff == 1 { // name
				fname = string(fv)
			}
			fb = rest2
		default:
			return "", 0, false
		}
	}
	return fname, fno, fname != "" && fno > 0
}

func TestFieldNumbersMatchDescriptor(t *testing.T) {
	msgs := loadDescriptorMessages(t)
	if len(msgs) < 100 {
		t.Fatalf("descriptor parse produced %d messages, expected hundreds", len(msgs))
	}
	// msg -> field name -> expected field number (as encoded in this package)
	checks := map[string]map[string]int{
		"AgentClientMessage": {"run_request": 1},
		"AgentRunRequest": {
			"conversation_state": 1, "action": 2, "model_details": 3,
			"mcp_tools": 4, "conversation_id": 5, "custom_system_prompt": 8, "requested_model": 9,
		},
		"ConversationStateStructure": {"root_prompt_messages_json": 1, "turns": 8, "mode": 10},
		"ConversationAction": {"user_message_action": 1, "resume_action": 2},
		"UserMessageAction": {"user_message": 1},
		"UserMessage":             {"text": 1, "message_id": 2, "mode": 4},
		"RequestedModel":          {"model_id": 1, "max_mode": 2},
		"McpTools":                {"mcp_tools": 1},
		"McpToolDefinition":       {"name": 1, "description": 2, "input_schema_json": 6},
		"AgentServerMessage":      {"interaction_update": 1, "conversation_checkpoint_update": 3, "interaction_query": 7},
		"InteractionUpdate": {
			"text_delta": 1, "tool_call_started": 2, "tool_call_completed": 3,
			"thinking_delta": 4, "thinking_completed": 5, "token_delta": 8,
			"heartbeat": 13, "turn_ended": 14, "tool_call_delta": 15,
			"step_started": 16, "step_completed": 17,
		},
		"ToolCallStartedUpdate":   {"call_id": 1},
		"ToolCallCompletedUpdate": {"call_id": 1},
		"ToolCallDeltaUpdate":     {"call_id": 1},
		"TextDeltaUpdate":         {"text": 1},
		"ThinkingDeltaUpdate":     {"text": 1},
		"TokenDeltaUpdate":        {"tokens": 1},
	}
	for msgName, want := range checks {
		fields, ok := msgs[msgName]
		if !ok {
			t.Errorf("descriptor missing message %s", msgName)
			continue
		}
		byName := make(map[string]int, len(fields))
		for no, fname := range fields {
			byName[fname] = no
		}
		for fname, wantNo := range want {
			if got := byName[fname]; got != wantNo {
				t.Errorf("%s.%s = field %d, want %d", msgName, fname, got, wantNo)
			}
		}
	}
}

func TestAgentClientMessageWireFormat(t *testing.T) {
	m := &AgentClientMessage{
		RunRequest: &AgentRunRequest{
			ConversationState: &ConversationStateStructure{
				RootPromptMessages: [][]byte{[]byte(`{"role":"user","content":"hi"}`)},
				Mode:               ConversationModeAgent,
			},
			Action: &ConversationAction{
				UserMessageAction: &UserMessageAction{
					UserMessage: &UserMessage{Text: "Reply: pong", MessageID: "m1"},
				},
			},
			RequestedModel: &RequestedModel{ModelID: "composer-2.5"},
			ConversationID: "conv-1",
		},
	}
	raw := m.Encode()
	if len(raw) == 0 {
		t.Fatal("empty encoding")
	}
	// Spot-check: conversation_id field 5 string "conv-1" appears verbatim.
	if !contains(raw, []byte("conv-1")) || !contains(raw, []byte("composer-2.5")) || !contains(raw, []byte("Reply: pong")) {
		t.Fatalf("expected literals missing from wire bytes: %x", raw)
	}
	// Walk top-level fields: all must be field 1 (run_request).
	key, rest := readVarintTB(raw)
	if int(key>>3) != 1 || int(key&7) != wireBytes {
		t.Fatalf("top-level field = %d wire %d, want 1/wireBytes", key>>3, key&7)
	}
	// Inside run_request, fields 1,2,5,9 must each appear exactly once.
	ln, restAfterLen := readVarintTB(rest)
	if restAfterLen == nil || uint64(len(restAfterLen)) < ln {
		t.Fatalf("truncated run_request")
	}
	b := restAfterLen[:ln]
	seen := map[int]int{}
	for len(b) > 0 {
		k, b2 := readVarintTB(b)
		if b2 == nil {
			t.Fatalf("malformed run_request body")
		}
		f, w := int(k>>3), int(k&7)
		b = b2
		if f == 10 && w == 0 { // mode is a varint field
			_, b3 := readVarintTB(b)
			if b3 == nil {
				t.Fatalf("malformed mode varint")
			}
			b = b3
			seen[f]++
			continue
		}
		if w != wireBytes {
			t.Fatalf("unexpected wire type %d for field %d", w, f)
		}
		l2, b4 := readVarintTB(b)
		if b4 == nil || uint64(len(b)) < l2 {
			t.Fatalf("malformed length for field %d", f)
		}
		b = b4[l2:]
		seen[f]++
	}
	for _, want := range []int{1, 2, 5, 9} {
		if seen[want] != 1 {
			t.Fatalf("run_request field %d seen %d times, want 1 (all: %v)", want, seen[want], seen)
		}
	}
}

func contains(haystack, needle []byte) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n []byte) int {
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func TestEnvelopeRoundTrip(t *testing.T) {
	payload := []byte{0x0a, 0x02, 'h', 'i'}
	frame := EncodeEnvelopeFrame(EnvelopeFlagData, payload)
	frames, err := DecodeEnvelopeFrames(frame)
	if err != nil {
		t.Fatalf("DecodeEnvelopeFrames: %v", err)
	}
	if len(frames) != 1 || frames[0].Flag != EnvelopeFlagData || !equal(frames[0].Payload, payload) {
		t.Fatalf("frames = %+v", frames)
	}

	multi := append(append([]byte{}, frame...), EncodeEnvelopeFrame(EnvelopeFlagEndStream, []byte(`{"error":{}}`))...)
	frames, err = DecodeEnvelopeFrames(multi)
	if err != nil || len(frames) != 2 || frames[1].Data() || !frames[0].Data() {
		t.Fatalf("multi frames = %+v err = %v", frames, err)
	}
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDecodeAgentServerMessage(t *testing.T) {
	// Build: AgentServerMessage{interaction_update{token_delta{tokens=42}, turn_ended{}}}
	turnEnded := &encoder{}
	_ = turnEnded
	// token_delta: field 8 { tokens=42 }
	td := &encoder{}
	td.varint(1, 42)
	// interaction_update: field 8 (token_delta), field 14 (turn_ended)
	upd := &encoder{}
	upd.message(8, &rawMarshaler{td.buf})
	empty := &encoder{}
	upd.message(14, &rawMarshaler{empty.buf})
	// agent server message: field 1
	top := &encoder{}
	top.message(1, &rawMarshaler{upd.buf})

	msg, err := DecodeAgentServerMessage(top.buf)
	if err != nil {
		t.Fatalf("DecodeAgentServerMessage: %v", err)
	}
	if msg.InteractionUpdate == nil {
		t.Fatal("interaction_update missing")
	}
	if !msg.InteractionUpdate.HasTokenDelta || msg.InteractionUpdate.TokenDelta != 42 {
		t.Fatalf("token delta = %d/%v", msg.InteractionUpdate.TokenDelta, msg.InteractionUpdate.HasTokenDelta)
	}
	if !msg.InteractionUpdate.TurnEnded {
		t.Fatal("turn_ended not set")
	}
}

func TestDecodeTextDeltaFrame(t *testing.T) {
	// text_delta{ text = "pong" }
	td := &encoder{}
	td.string(1, "pong")
	upd := &encoder{}
	upd.message(1, &rawMarshaler{td.buf})
	top := &encoder{}
	top.message(1, &rawMarshaler{upd.buf})
	msg, err := DecodeAgentServerMessage(top.buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !msg.InteractionUpdate.HasTextDelta || msg.InteractionUpdate.TextDelta != "pong" {
		t.Fatalf("text delta = %q", msg.InteractionUpdate.TextDelta)
	}
}

func TestDecodeGetUsableModelsResponse(t *testing.T) {
	// Two ModelDetails entries.
	m1 := &encoder{}
	m1.string(1, "composer-2.5")
	m1.string(4, "Composer 2.5")
	m2 := &encoder{}
	m2.string(1, "grok-4.6")
	m2.bool(7, true)
	top := &encoder{}
	top.message(1, &rawMarshaler{m1.buf})
	top.message(1, &rawMarshaler{m2.buf})
	models, err := DecodeGetUsableModelsResponse(top.buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d", len(models))
	}
	if models[0].ModelID != "composer-2.5" || models[0].DisplayName != "Composer 2.5" {
		t.Fatalf("m0 = %+v", models[0])
	}
	if models[1].ModelID != "grok-4.6" || !models[1].MaxMode {
		t.Fatalf("m1 = %+v", models[1])
	}
}

func TestPromptMessageJSON(t *testing.T) {
	p := &PromptMessage{Role: "user", Content: "say \"hi\"\n\ttab"}
	out := p.EncodeJSON()
	want := `{"role":"user","content":"say \"hi\"\n\ttab"}`
	if string(out) != want {
		t.Fatalf("json = %s, want %s", out, want)
	}
}

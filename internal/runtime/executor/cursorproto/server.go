package cursorproto

// AgentServerMessage is the top-level full-duplex frame the server sends back
// from AgentService.Run.
type AgentServerMessage struct {
	InteractionUpdate      *InteractionUpdate
	ConversationCheckpoint []byte // serialized ConversationStateStructure (opaque)
	InteractionQuery       *InteractionQuery
	KvServer               *KvServerMessage
	ExecServer             *ExecServerMessage
	Raw                    []byte
}

// KvServerMessage is the server-side blob storage channel.
type KvServerMessage struct {
	ID        uint32
	GetBlobID []byte
	WantGet   bool
	SetBlobID []byte
	SetBlob   []byte
	WantSet   bool
}

// ExecServerMessage carries server-initiated execution requests.
type ExecServerMessage struct {
	ID                 uint32
	ExecID             string
	WantRequestContext bool
	Raw                []byte
}

// DecodeAgentServerMessage parses one server frame.
func DecodeAgentServerMessage(data []byte) (*AgentServerMessage, error) {
	m := &AgentServerMessage{Raw: append([]byte(nil), data...)}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireBytes:
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			upd, err3 := decodeInteractionUpdate(raw)
			if err3 != nil {
				return nil, err3
			}
			m.InteractionUpdate = upd
			b = b2
		case field == 4 && wire == wireBytes:
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			kv, errKv := decodeKvServerMessage(raw)
			if errKv != nil {
				return nil, errKv
			}
			m.KvServer = kv
			b = b2
		case field == 2 && wire == wireBytes:
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			ex, errEx := decodeExecServerMessage(raw)
			if errEx != nil {
				return nil, errEx
			}
			m.ExecServer = ex
			b = b2
		case field == 3 && wire == wireBytes:
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			m.ConversationCheckpoint = raw
			b = b2
		case field == 7 && wire == wireBytes:
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			m.InteractionQuery = &InteractionQuery{Raw: raw}
			b = b2
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

// InteractionUpdate is one streaming event inside AgentServerMessage.
type InteractionUpdate struct {
	TextDelta         string
	HasTextDelta      bool
	ThinkingDelta     string
	HasThinkingDelta  bool
	ThinkingCompleted bool
	ToolCallStarted   *ToolCallEvent
	ToolCallDelta     *ToolCallDeltaEvent
	ToolCallCompleted *ToolCallEvent
	TokenDelta        int32
	HasTokenDelta     bool
	TurnEnded         bool
	Heartbeat         bool
	StepStarted       bool
	StepCompleted     bool
	Summary           string
	HasSummary        bool
}

func decodeInteractionUpdate(data []byte) (*InteractionUpdate, error) {
	upd := &InteractionUpdate{}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		if wire != wireBytes {
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
			continue
		}
		raw, b2, err2 := readBytes(rest)
		if err2 != nil {
			return nil, err2
		}
		b = b2
		switch field {
		case 1: // text_delta.text
			upd.TextDelta, _, err = decodeStringField(raw, 1)
			if err != nil {
				return nil, err
			}
			upd.HasTextDelta = true
		case 4: // thinking_delta.text
			upd.ThinkingDelta, _, err = decodeStringField(raw, 1)
			if err != nil {
				return nil, err
			}
			upd.HasThinkingDelta = true
		case 5: // thinking_completed
			upd.ThinkingCompleted = true
		case 2: // tool_call_started
			ev, err2 := decodeToolCallEvent(raw)
			if err2 != nil {
				return nil, err2
			}
			upd.ToolCallStarted = ev
		case 15: // tool_call_delta
			ev, err2 := decodeToolCallDeltaEvent(raw)
			if err2 != nil {
				return nil, err2
			}
			upd.ToolCallDelta = ev
		case 3: // tool_call_completed
			ev, err2 := decodeToolCallEvent(raw)
			if err2 != nil {
				return nil, err2
			}
			upd.ToolCallCompleted = ev
		case 8: // token_delta.tokens
			v, _, err2 := decodeVarintField(raw, 1)
			if err2 != nil {
				return nil, err2
			}
			upd.TokenDelta = int32(v)
			upd.HasTokenDelta = true
		case 14: // turn_ended
			upd.TurnEnded = true
		case 13: // heartbeat
			upd.Heartbeat = true
		case 16: // step_started
			upd.StepStarted = true
		case 17: // step_completed
			upd.StepCompleted = true
		case 9: // summary.text
			upd.Summary, _, err = decodeStringField(raw, 1)
			if err != nil {
				return nil, err
			}
			upd.HasSummary = true
		default:
			// unknown update kind: ignored
		}
	}
	return upd, nil
}

// ToolCallEvent describes a tool call start/completion.
type ToolCallEvent struct {
	CallID      string
	ModelCallID string
	// For completed MCP tool calls: the raw ToolCall blob (opaque passthrough).
	ToolCallRaw []byte
	// ArgsJSON carries the arguments JSON for MCP calls when extractable.
	ArgsJSON string
}

func decodeToolCallEvent(data []byte) (*ToolCallEvent, error) {
	ev := &ToolCallEvent{}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireBytes: // call_id
			ev.CallID, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		case field == 2 && wire == wireBytes: // tool_call
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			ev.ToolCallRaw = raw
			b = b2
		case field == 3 && wire == wireBytes: // model_call_id
			ev.ModelCallID, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return ev, nil
}

// ToolCallDeltaEvent carries incremental tool call arguments.
type ToolCallDeltaEvent struct {
	CallID      string
	ArgsPartial string
}

func decodeToolCallDeltaEvent(data []byte) (*ToolCallDeltaEvent, error) {
	ev := &ToolCallDeltaEvent{}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireBytes: // call_id
			ev.CallID, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		case field == 2 && wire == wireBytes: // tool_call_delta.tool_call_delta.edit_tool_call_delta... keep raw text
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			ev.ArgsPartial = extractDeepestString(raw)
			b = b2
		case field == 3 && wire == wireBytes: // model_call_id
			_, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return ev, nil
}

// InteractionQuery is a server-initiated query (e.g. ask user). Treated as
// unsupported in V1; kept for error reporting.
type InteractionQuery struct {
	Raw []byte
}

// decodeStringField reads a length-delimited field's inner string field.
func decodeStringField(data []byte, wantField int) (string, []byte, error) {
	var out string
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return "", nil, err
		}
		if field == wantField && wire == wireBytes {
			out, b, err = readString(rest)
			if err != nil {
				return "", nil, err
			}
		} else {
			b, err = skipField(rest, wire)
			if err != nil {
				return "", nil, err
			}
		}
	}
	return out, b, nil
}

func decodeVarintField(data []byte, wantField int) (uint64, []byte, error) {
	var out uint64
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return 0, nil, err
		}
		if field == wantField && wire == wireVarint {
			out, b, err = readVarint(rest)
			if err != nil {
				return 0, nil, err
			}
		} else {
			b, err = skipField(rest, wire)
			if err != nil {
				return 0, nil, err
			}
		}
	}
	return out, b, nil
}

// extractDeepestString returns the last string field found anywhere in the
// nested blob — used for tool-call argument partials whose nesting varies.
func extractDeepestString(data []byte) string {
	var last string
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return last
		}
		switch wire {
		case wireBytes:
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return last
			}
			if field == 1 && isUTF8(raw) {
				last = string(raw)
			} else if nested := extractDeepestString(raw); nested != "" {
				last = nested
			}
			b = b2
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return last
			}
		}
	}
	return last
}

func isUTF8(b []byte) bool {
	for _, c := range b {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
	}
	return len(b) > 0
}

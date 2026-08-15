package cursorproto

// ParseMcpToolCall extracts the MCP tool name and arguments JSON from a raw
// ToolCall blob (as carried by ToolCallStartedUpdate/ToolCallCompletedUpdate).
//
// ToolCall layout (from the descriptor):
//   mcp_tool_call = field 15 (McpToolCall)
// McpToolCall layout:
//   args = field 1 (McpArgs)
//   description = field 3
// McpArgs layout:
//   server_identifier = field 1
//   tool_name = field 2
//   arguments_json = field 3 (best-effort, observed as JSON string)
//   tool_call_id = field 6
//
// Returns ("", nil, false-style zeros) when the blob is not an MCP call.
func ParseMcpToolCall(toolCallRaw []byte) (name string, argsJSON []byte) {
	b := toolCallRaw
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return "", nil
		}
		if field == 15 && wire == wireBytes {
			raw, _, err2 := readBytes(rest)
			if err2 != nil {
				return "", nil
			}
			return parseMcpToolCall(raw)
		}
		b, err = skipField(rest, wire)
		if err != nil {
			return "", nil
		}
	}
	return "", nil
}

func parseMcpToolCall(data []byte) (string, []byte) {
	var name string
	var args []byte
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return name, args
		}
		if wire != wireBytes {
			b, err = skipField(rest, wire)
			if err != nil {
				return name, args
			}
			continue
		}
		raw, b2, err2 := readBytes(rest)
		if err2 != nil {
			return name, args
		}
		b = b2
		if field == 1 { // args
			n, a := parseMcpArgs(raw)
			if n != "" {
				name = n
			}
			if a != nil {
				args = a
			}
		}
	}
	return name, args
}

func parseMcpArgs(data []byte) (string, []byte) {
	var name string
	var args []byte
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return name, args
		}
		switch {
		case field == 2 && wire == wireBytes: // tool_name
			name, b, err = readString(rest)
			if err != nil {
				return name, args
			}
		case field == 3 && wire == wireBytes: // arguments_json
			var s string
			s, b, err = readString(rest)
			if err != nil {
				return name, args
			}
			args = []byte(s)
		default:
			if wire == wireBytes {
				var raw []byte
				raw, b, err = readBytes(rest)
				if err != nil {
					return name, args
				}
				_ = raw
			} else {
				b, err = skipField(rest, wire)
				if err != nil {
					return name, args
				}
			}
		}
	}
	return name, args
}

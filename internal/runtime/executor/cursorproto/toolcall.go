package cursorproto

import "encoding/json"

// ParseMcpToolCall extracts the MCP tool name and arguments JSON from a raw
// ToolCall blob (as carried by ToolCallStartedUpdate/ToolCallCompletedUpdate).
//
// Observed ToolCall layout (live traffic, descriptor-corroborated):
//
//	mcp_tool_call = field 15 (McpToolCall)
//
// McpToolCall layout:
//
//	call_spec = field 1 (message)
//
// CallSpec layout:
//
//	tool_name = field 1 (string)
//	args = field 2, repeated (map entry: key = field 1 string,
//	         value = field 2 message carrying the scalar at field 3)
//	tool_call_id = field 3 (string)
//
// Returns ("", nil) when the blob is not an MCP call.
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
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			break
		}
		if wire != wireBytes {
			b, err = skipField(rest, wire)
			if err != nil {
				break
			}
			continue
		}
		raw, b2, err2 := readBytes(rest)
		if err2 != nil {
			break
		}
		b = b2
		if field == 1 { // call spec
			name, args := parseMcpCallSpec(raw)
			if name == "" {
				return "", nil
			}
			if len(args) == 0 {
				return name, []byte("{}")
			}
			out, errM := json.Marshal(args)
			if errM != nil {
				return name, []byte("{}")
			}
			return name, out
		}
	}
	return "", nil
}

// parseMcpCallSpec decodes the nested call specification: tool_name = field 1
// (string), args = field 2 (repeated map entries), tool_call_id = field 3.
func parseMcpCallSpec(data []byte) (string, map[string]any) {
	var name string
	args := map[string]any{}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			break
		}
		if wire != wireBytes {
			b, err = skipField(rest, wire)
			if err != nil {
				break
			}
			continue
		}
		raw, b2, err2 := readBytes(rest)
		if err2 != nil {
			break
		}
		b = b2
		switch field {
		case 1:
			if name == "" {
				name = string(raw)
			}
		case 2:
			if k, v, ok := parseMcpArgEntry(raw); ok {
				args[k] = v
			}
		}
	}
	return name, args
}

// parseMcpArgEntry decodes one args map entry: key = field 1 string, value =
// field 2 message carrying the scalar at field 3.
func parseMcpArgEntry(data []byte) (string, any, bool) {
	var key string
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			break
		}
		if wire != wireBytes {
			b, err = skipField(rest, wire)
			if err != nil {
				break
			}
			continue
		}
		raw, b2, err2 := readBytes(rest)
		if err2 != nil {
			break
		}
		b = b2
		if field == 1 {
			key = string(raw)
		} else if field == 2 {
			value := parseMcpArgValue(raw)
			if value == "" && len(raw) > 0 && isUTF8(raw) {
				// The descriptor declares ArgsEntry.value as plain bytes;
				// scalar arguments (numbers, bools) can arrive as their
				// literal byte representation instead of a nested message.
				value = string(raw)
			}
			if value != "" && key != "" {
				return key, value, true
			}
		}
	}
	return "", nil, false
}

// parseMcpArgValue returns the scalar value of an arg-entry value message:
// the first length-delimited non-empty UTF-8 payload found (observed at
// field 3).
func parseMcpArgValue(data []byte) string {
	b := data
	for len(b) > 0 {
		_, wire, rest, err := fieldHeader(b)
		if err != nil {
			return ""
		}
		if wire != wireBytes {
			b, err = skipField(rest, wire)
			if err != nil {
				return ""
			}
			continue
		}
		raw, b2, err2 := readBytes(rest)
		if err2 != nil {
			return ""
		}
		b = b2
		if len(raw) > 0 && isUTF8(raw) {
			return string(raw)
		}
	}
	return ""
}

// ParseShellToolCall extracts the command from a native shell_tool_call
// (ToolCall field 1 -> ShellToolCall field 1 -> ShellArgs field 1).
// Returns ok=false when the blob is not a shell call.
func ParseShellToolCall(toolCallRaw []byte) (command string, ok bool) {
	b := toolCallRaw
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return "", false
		}
		if field == 1 && wire == wireBytes {
			raw, _, err2 := readBytes(rest)
			if err2 != nil {
				return "", false
			}
			return parseShellToolCall(raw)
		}
		b, err = skipField(rest, wire)
		if err != nil {
			return "", false
		}
	}
	return "", false
}

func parseShellToolCall(data []byte) (string, bool) {
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return "", false
		}
		if field == 1 && wire == wireBytes { // ShellArgs
			raw, _, err2 := readBytes(rest)
			if err2 != nil {
				return "", false
			}
			if cmd, ok := parseShellArgs(raw); ok {
				return cmd, true
			}
			return "", false
		}
		b, err = skipField(rest, wire)
		if err != nil {
			return "", false
		}
	}
	return "", false
}

func parseShellArgs(data []byte) (string, bool) {
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return "", false
		}
		if field == 1 && wire == wireBytes { // command
			raw, _, err2 := readBytes(rest)
			if err2 != nil {
				return "", false
			}
			return string(raw), true
		}
		b, err = skipField(rest, wire)
		if err != nil {
			return "", false
		}
	}
	return "", false
}

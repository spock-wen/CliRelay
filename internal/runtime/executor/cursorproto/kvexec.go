package cursorproto

// KvServerMessage layout (from descriptor):
//   id = 1 (uint32)
//   message oneof:
//     getBlobArgs = 1 { blobId = 1 (bytes) }
//     setBlobArgs = 2 { blobId = 1 (bytes), blobData = 2 (bytes) }
// The oneof fields are nested inside message field 2? No — descriptor shows
// KvServerMessage { id=1, get_blob_args=1... } with the oneof collapsed into
// the parent; observed traffic puts the oneof variant at the parent level.

// decodeKvServerMessage parses the KV channel message.
func decodeKvServerMessage(data []byte) (*KvServerMessage, error) {
	kv := &KvServerMessage{}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireVarint:
			v, b2, err2 := readVarint(rest)
			if err2 != nil {
				return nil, err2
			}
			kv.ID = uint32(v)
			b = b2
		case field == 2 && wire == wireBytes:
			// get_blob_args { blob_id = 1 }
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			blobID, _, err3 := decodeBytesField(raw, 1)
			if err3 != nil {
				return nil, err3
			}
			kv.GetBlobID = blobID
			kv.WantGet = true
			b = b2
		case field == 3 && wire == wireBytes:
			// set_blob_args { blob_id = 1, blob_data = 2 }
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			blobID, blobData, err3 := decodeSetBlobArgs(raw)
			if err3 != nil {
				return nil, err3
			}
			kv.SetBlobID = blobID
			kv.SetBlob = blobData
			kv.WantSet = true
			b = b2
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return kv, nil
}

func decodeBytesField(data []byte, wantField int) ([]byte, []byte, error) {
	var out []byte
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, nil, err
		}
		if field == wantField && wire == wireBytes {
			out, b, err = readBytes(rest)
			if err != nil {
				return nil, nil, err
			}
		} else {
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return out, b, nil
}

func decodeSetBlobArgs(data []byte) (blobID, blobData []byte, err error) {
	b := data
	for len(b) > 0 {
		field, wire, rest, err2 := fieldHeader(b)
		if err2 != nil {
			return nil, nil, err2
		}
		switch {
		case field == 1 && wire == wireBytes:
			blobID, b, err = readBytes(rest)
		case field == 2 && wire == wireBytes:
			blobData, b, err = readBytes(rest)
		default:
			b, err = skipField(rest, wire)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return blobID, blobData, nil
}

// ExecServerMessage layout:
//   id = 1 (uint32), exec_id = 15 (string)
//   request_context_args = 10 { notes_session_id=2, workspace_id=3, ... }

// decodeExecServerMessage parses the exec channel message.
func decodeExecServerMessage(data []byte) (*ExecServerMessage, error) {
	ex := &ExecServerMessage{Raw: append([]byte(nil), data...)}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireVarint:
			v, b2, err2 := readVarint(rest)
			if err2 != nil {
				return nil, err2
			}
			ex.ID = uint32(v)
			b = b2
		case field == 15 && wire == wireBytes:
			var err2 error
			ex.ExecID, b, err2 = readString(rest)
			if err2 != nil {
				return nil, err2
			}
		case field == 10 && wire == wireBytes:
			// requestContextArgs: presence is all V1 needs.
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			_ = raw
			ex.WantRequestContext = true
			b = b2
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return ex, nil
}

// UnwrapBlobID extracts the innermost bytes payload from a content-addressed
// blob id. Observed shapes:
//   - {1:{1:{1:<text>}}} (triple-nested wrapper for inline text)
//   - {1:<json>} (single wrapper for serialized prompt messages)
// Returns nil for unrecognized shapes.
func UnwrapBlobID(blobID []byte) []byte {
	if inner := unwrapNested(blobID, 3); inner != nil {
		return inner
	}
	return unwrapNested(blobID, 1)
}

func unwrapNested(b []byte, depth int) []byte {
	for range depth {
		field, wire, rest, err := fieldHeader(b)
		if err != nil || field != 1 || wire != wireBytes {
			return nil
		}
		raw, _, err2 := readBytes(rest)
		if err2 != nil {
			return nil
		}
		b = raw
	}
	return b
}

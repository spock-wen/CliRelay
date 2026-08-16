package cursorproto

import (
	"encoding/binary"
	"fmt"
)

// Connect streaming envelope framing:
//   frame = 1-byte flag + 4-byte big-endian length + payload
//   flag 0 = data (protobuf), flag 2 = end-stream (JSON error).

// EnvelopeFlags
const (
	EnvelopeFlagData       = 0
	EnvelopeFlagEndStream  = 2
)

// EncodeEnvelopeFrame wraps a payload in a Connect streaming frame.
func EncodeEnvelopeFrame(flag byte, payload []byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = flag
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}

// EnvelopeFrame is one decoded frame.
type EnvelopeFrame struct {
	Flag    byte
	Payload []byte
}

// Data reports whether this frame carries a protobuf message.
func (f *EnvelopeFrame) Data() bool { return f.Flag == EnvelopeFlagData }

// DecodeEnvelopeFrames parses a full buffer of Connect frames.
func DecodeEnvelopeFrames(data []byte) ([]EnvelopeFrame, error) {
	var frames []EnvelopeFrame
	for len(data) > 0 {
		if len(data) < 5 {
			return nil, fmt.Errorf("cursorproto: truncated envelope header (%d bytes)", len(data))
		}
		flag := data[0]
		n := binary.BigEndian.Uint32(data[1:5])
		if uint64(len(data)-5) < uint64(n) {
			return nil, fmt.Errorf("cursorproto: truncated envelope payload (want %d, have %d)", n, len(data)-5)
		}
		frames = append(frames, EnvelopeFrame{
			Flag:    flag,
			Payload: data[5 : 5+n],
		})
		data = data[5+n:]
	}
	return frames, nil
}

// ModelDetailsInfo is one entry of GetUsableModelsResponse.
type ModelDetailsInfo struct {
	ModelID         string
	DisplayName     string
	DisplayModelID  string
	MaxMode         bool
}

// DecodeGetUsableModelsResponse parses the unary response (application/proto,
// no envelope).
func DecodeGetUsableModelsResponse(data []byte) ([]ModelDetailsInfo, error) {
	var out []ModelDetailsInfo
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		if field == 1 && wire == wireBytes {
			raw, b2, err2 := readBytes(rest)
			if err2 != nil {
				return nil, err2
			}
			info, err3 := decodeModelDetails(raw)
			if err3 != nil {
				return nil, err3
			}
			out = append(out, *info)
			b = b2
		} else {
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func decodeModelDetails(data []byte) (*ModelDetailsInfo, error) {
	info := &ModelDetailsInfo{}
	b := data
	for len(b) > 0 {
		field, wire, rest, err := fieldHeader(b)
		if err != nil {
			return nil, err
		}
		switch {
		case field == 1 && wire == wireBytes: // model_id
			info.ModelID, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		case field == 4 && wire == wireBytes: // display_name
			info.DisplayName, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		case field == 3 && wire == wireBytes: // display_model_id
			info.DisplayModelID, b, err = readString(rest)
			if err != nil {
				return nil, err
			}
		case field == 7 && wire == wireVarint: // max_mode
			v, b2, err2 := readVarint(rest)
			if err2 != nil {
				return nil, err2
			}
			info.MaxMode = v != 0
			b = b2
		default:
			b, err = skipField(rest, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return info, nil
}

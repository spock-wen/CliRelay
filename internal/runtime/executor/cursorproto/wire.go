// Package cursorproto implements the subset of Cursor's agent.v1 protobuf
// protocol needed to run chat completions through the AgentService.Run RPC.
//
// The protocol is Cursor-internal (no published .proto). Field numbers below
// are extracted from the descriptor shipped with the Cursor CLI; the binary
// descriptor is kept at the repo root (.cursor-agent-descriptor.bin) and the
// contract test verifies our wire format matches it byte-for-byte.
package cursorproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

// wire types
const (
	wireVarint = 0
	wireFixed64 = 1
	wireBytes  = 2
	wireFixed32 = 5
)

// encoder accumulates protobuf output.
type encoder struct {
	buf []byte
}

func (e *encoder) varint(field int, v uint64) {
	e.tag(field, wireVarint)
	e.buf = binary.AppendUvarint(e.buf, v)
}

func (e *encoder) bool(field int, v bool) {
	if v {
		e.varint(field, 1)
	} else {
		e.varint(field, 0)
	}
}

func (e *encoder) int32(field int, v int32) {
	e.varint(field, uint64(v))
}

func (e *encoder) uint32(field int, v uint32) {
	e.varint(field, uint64(v))
}

func (e *encoder) string(field int, v string) {
	e.tag(field, wireBytes)
	e.buf = binary.AppendUvarint(e.buf, uint64(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *encoder) bytes(field int, v []byte) {
	e.tag(field, wireBytes)
	e.buf = binary.AppendUvarint(e.buf, uint64(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *encoder) message(field int, m marshaler) {
	if m == nil || isNilMarshaler(m) {
		return
	}
	sub := m.encode()
	e.tag(field, wireBytes)
	e.buf = binary.AppendUvarint(e.buf, uint64(len(sub)))
	e.buf = append(e.buf, sub...)
}

// isNilMarshaler reports whether m is a typed nil pointer (e.g. (*ModelDetails)(nil)).
func isNilMarshaler(m marshaler) bool {
	rv := reflect.ValueOf(m)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

func (e *encoder) tag(field, wire int) {
	e.buf = binary.AppendUvarint(e.buf, uint64(field)<<3|uint64(wire))
}

type marshaler interface {
	encode() []byte
}

// fieldHeader returns (fieldNumber, wireType) or an error at end of buffer.
func fieldHeader(b []byte) (field int, wire int, rest []byte, err error) {
	key, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0, nil, errors.New("cursorproto: truncated field key")
	}
	b = b[n:]
	field = int(key >> 3)
	wire = int(key & 7)
	if field == 0 {
		return 0, 0, nil, errors.New("cursorproto: invalid field number 0")
	}
	return field, wire, b, nil
}

func readVarint(b []byte) (uint64, []byte, error) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, nil, errors.New("cursorproto: truncated varint")
	}
	return v, b[n:], nil
}

func readBytes(b []byte) ([]byte, []byte, error) {
	ln, b, err := readVarint(b)
	if err != nil {
		return nil, nil, err
	}
	if uint64(len(b)) < ln {
		return nil, nil, fmt.Errorf("cursorproto: truncated bytes field (want %d, have %d)", ln, len(b))
	}
	return b[:ln], b[ln:], nil
}

func readString(b []byte) (string, []byte, error) {
	raw, b, err := readBytes(b)
	if err != nil {
		return "", nil, err
	}
	return string(raw), b, nil
}

func skipField(b []byte, wire int) ([]byte, error) {
	switch wire {
	case wireVarint:
		_, b, err := readVarint(b)
		return b, err
	case wireFixed64:
		if len(b) < 8 {
			return nil, errors.New("cursorproto: truncated fixed64")
		}
		return b[8:], nil
	case wireBytes:
		_, b, err := readBytes(b)
		return b, err
	case wireFixed32:
		if len(b) < 4 {
			return nil, errors.New("cursorproto: truncated fixed32")
		}
		return b[4:], nil
	default:
		return nil, fmt.Errorf("cursorproto: unsupported wire type %d", wire)
	}
}

package executor

import (
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor/cursorproto"
)

// cursorSession holds per-stream protocol state: the KV blob store the server
// populates via setBlob and reads back via getBlob, plus the tool definitions
// reported in requestContext.
type cursorSession struct {
	mu    sync.Mutex
	blobs map[string][]byte
	tools []*cursorproto.McpToolDefinition
}

func newCursorSession(tools []*cursorproto.McpToolDefinition) *cursorSession {
	return &cursorSession{
		blobs: make(map[string][]byte),
		tools: tools,
	}
}

func (s *cursorSession) get(blobID []byte) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.blobs[string(blobID)]
	return data, ok
}

// Put stores a blob keyed by its hash (alias used during request build).
func (s *cursorSession) Put(blobID, data []byte) { s.set(blobID, data) }

func (s *cursorSession) set(blobID, data []byte) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[string(blobID)] = data
}

// protocolReply builds the client response for server-initiated KV/exec
// messages. Returns nil when the message needs no reply.
func (t *cursorTransport) protocolReply(msg *cursorproto.AgentServerMessage) *cursorproto.AgentClientMessage {
	if msg == nil {
		return nil
	}
	if kv := msg.KvServer; kv != nil {
		switch {
		case kv.WantGet:
			data, _ := t.session.get(kv.GetBlobID)
			if data == nil {
				// Content-addressed fallback: the blob id embeds the payload
				// ({field1:{field1:{field1:<text>}}}); unwrap it so first-turn
				// gets resolve without a prior set.
				if inner := cursorproto.UnwrapBlobID(kv.GetBlobID); inner != nil {
					data = inner
				} else {
					data = []byte{}
				}
			}
			return &cursorproto.AgentClientMessage{
				KvClientMessage: &cursorproto.KvClientMessage{
					ID:          kv.ID,
					GetBlobData: data,
					HaveGetBlob: true,
				},
			}
		case kv.WantSet:
			t.session.set(kv.SetBlobID, kv.SetBlob)
			return &cursorproto.AgentClientMessage{
				KvClientMessage: &cursorproto.KvClientMessage{
					ID:         kv.ID,
					SetBlobDone: true,
				},
			}
		}
		return nil
	}
	if ex := msg.ExecServer; ex != nil && ex.WantRequestContext {
		ctxResult := &cursorproto.RequestContextFull{
			Context: &cursorproto.RequestContext{
				Env: &cursorproto.RequestContextEnv{
					OSVersion:      "Darwin 25.5.0 arm64",
					WorkspacePaths: "/tmp",
					Shell:          "/bin/zsh",
					TimeZone:       "Asia/Shanghai",
					ProjectFolder:  "/tmp",
				},
			},
		}
		if len(t.session.tools) > 0 {
			ctxResult.Context.Tools = t.session.tools
		}
		return &cursorproto.AgentClientMessage{
			ExecClientMessage: &cursorproto.ExecClientMessage{
				ID:                  ex.ID,
				RequestContextResult: ctxResult,
			},
		}
	}
	if ex := msg.ExecServer; ex != nil && ex.ID != 0 {
		// Unknown exec variant: fail fast with an explicit error result.
		return &cursorproto.AgentClientMessage{
			ExecClientMessage: &cursorproto.ExecClientMessage{
				ID:          ex.ID,
				ErrorMessage: "unsupported exec variant",
			},
		}
	}
	return nil
}

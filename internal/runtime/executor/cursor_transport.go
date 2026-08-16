package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/net/http2"

	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor/cursorproto"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// cursorConnectError is a Connect end-stream error decoded from the JSON
// trailer frame of a Run response.
type cursorConnectError struct {
	Code    string             `json:"code"`
	Message string             `json:"message"`
	Details []cursorErrorValue `json:"details"`
}

type cursorErrorValue struct {
	Type  string          `json:"type"`
	Debug json.RawMessage `json:"debug"`
}

// cursorDebugDetails mirrors aiserver.v1.ErrorDetails.debug.details.
type cursorDebugDetails struct {
	Error  string `json:"error"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type cursorErrorDebug struct {
	Details cursorDebugDetails `json:"details"`
}

// Region reports whether the error is a region availability block (not quota,
// not retryable on this account).
func (e *cursorConnectError) Region() bool {
	if e == nil {
		return false
	}
	for _, d := range e.Details {
		var dbg cursorErrorDebug
		if errJSON := json.Unmarshal(d.Debug, &dbg); errJSON != nil {
			continue
		}
		if strings.EqualFold(dbg.Details.Title, "Model not available") &&
			strings.Contains(strings.ToLower(dbg.Details.Detail), "region") {
			return true
		}
	}
	return false
}

func (e *cursorConnectError) Error() string {
	if e == nil {
		return "cursor: unknown error"
	}
	for _, d := range e.Details {
		var dbg cursorErrorDebug
		if errJSON := json.Unmarshal(d.Debug, &dbg); errJSON == nil && dbg.Details.Title != "" {
			return fmt.Sprintf("cursor: %s: %s (%s)", e.Code, dbg.Details.Title, dbg.Details.Detail)
		}
	}
	return fmt.Sprintf("cursor: %s: %s", e.Code, e.Message)
}

// cursorTransport issues AgentService RPCs over HTTP/2 with Connect framing.
type cursorTransport struct {
	cfg     *config.Config
	client  *http.Client
	session *cursorSession
}

func newCursorTransport(cfg *config.Config, auth *cliproxyauth.Auth) *cursorTransport {
	var proxyStr string
	if cfg != nil {
		proxyID := ""
		fallbackURL := ""
		if auth != nil {
			proxyID = auth.ProxyID
			fallbackURL = auth.ProxyURL
		}
		proxyStr = strings.TrimSpace(cfg.ResolveProxyURL(proxyID, fallbackURL))
	} else if auth != nil {
		proxyStr = strings.TrimSpace(auth.ProxyURL)
	}
	transport := &http2.Transport{}
	if proxyStr != "" {
		parsed, err := url.Parse(proxyStr)
		if err != nil {
			log.Warnf("cursor: ignore invalid proxy URL: %v", err)
		} else if dial := proxyDialTLSContext(parsed); dial != nil {
			transport.DialTLSContext = dial
		} else {
			log.Warnf("cursor: proxy scheme %q unsupported for http2, dialing directly", parsed.Scheme)
		}
	}
	return &cursorTransport{
		cfg:     cfg,
		client:  &http.Client{Transport: transport},
		session: newCursorSession(nil),
	}
}

func cursorClientVersion(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.CursorClientVersion) != "" {
		return strings.TrimSpace(cfg.CursorClientVersion)
	}
	return cursorauth.ClientVersion
}
func cursorUserAgent(cfg *config.Config) string {
	return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Cursor/%s Chrome/132.0.6834.210 Electron/34.3.4 Safari/537.36", cursorClientVersion(cfg))
}

func (t *cursorTransport) headers(token string, contentType string) http.Header {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	h.Set("Connect-Protocol-Version", "1")
	h.Set("TE", "trailers")
	h.Set("Authorization", "Bearer "+token)
	h.Set("X-Ghost-Mode", "true")
	h.Set("X-Cursor-Client-Version", cursorClientVersion(t.cfg))
	h.Set("X-Cursor-Client-Type", cursorauth.ClientType)
	h.Set("X-Request-Id", uuid.NewString())
	h.Set("User-Agent", cursorUserAgent(t.cfg))
	h.Set("Accept-Encoding", "identity")
	return h
}

// cursorStreamEvent is one decoded server frame.
type cursorStreamEvent struct {
	Message *cursorproto.AgentServerMessage
	Err     error
}

// cursorEndpointFrameLen returns the full frame size for a buffered prefix.
func cursorEndpointFrameLen(buf []byte) (int, bool) {
	if len(buf) < 5 {
		return 0, false
	}
	n := int(uint32(buf[1])<<24 | uint32(buf[2])<<16 | uint32(buf[3])<<8 | uint32(buf[4]))
	return 5 + n, true
}

// RunStream opens a full-duplex Run RPC, sends the client message, and returns
// a channel of decoded server events. The channel closes when the stream ends.
func (t *cursorTransport) RunStream(ctx context.Context, baseURL, token string, msg *cursorproto.AgentClientMessage) (<-chan cursorStreamEvent, http.Header, error) {
	if t.session == nil {
		t.session = newCursorSession(nil)
	}
	if msg != nil && msg.RunRequest != nil && msg.RunRequest.McpTools != nil {
		t.session.tools = msg.RunRequest.McpTools.Tools
	}
	endpoint := strings.TrimSuffix(baseURL, "/") + "/agent.v1.AgentService/Run"
	frame := cursorproto.EncodeEnvelopeFrame(cursorproto.EnvelopeFlagData, msg.Encode())

	// Full-duplex: the Cursor backend treats request-body EOF as session end,
	// so keep the write side open via a pipe. io.Pipe is unbuffered, so the
	// frame write must run in its own goroutine.
	pr, pw := io.Pipe()
	writeErr := make(chan error, 1)
	go func() {
		_, errWrite := pw.Write(frame)
		writeErr <- errWrite
	}()

	// Protocol replies (kv blob echo, context results) must be written in
	// order but must never block the read loop: a stalled pw.Write — the
	// upstream not reading our reply while it keeps streaming frames —
	// freezes frame decoding and turns incremental deltas into one burst.
	// replies serializes writes on a background goroutine.
	replyCh := make(chan []byte, 32)
	go func() {
		// Exits when the pipe is closed (ctx cancel or stream end) and the
		// pending Write fails; drains senders until they observe ctx.Done.
		for buf := range replyCh {
			if _, errWrite := pw.Write(buf); errWrite != nil {
				for range replyCh {
				}
				return
			}
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		_ = pw.Close()
		return nil, nil, err
	}
	req.Header = t.headers(token, "application/connect+proto")
	resp, err := t.client.Do(req)
	if err != nil {
		_ = pw.Close()
		return nil, nil, err
	}
	go func() {
		<-ctx.Done()
		_ = pw.CloseWithError(context.Canceled)
		<-writeErr
	}()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		_ = pw.Close()
		return nil, nil, fmt.Errorf("cursor run: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	out := make(chan cursorStreamEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(out)
		defer close(done)
		// The read loop is the only replyCh sender; closing it here lets the
		// reply writer exit after the last in-flight reply is written.
		defer close(replyCh)
		var frameBuf []byte
		for {
			chunk := make([]byte, 32*1024)
			n, errRead := resp.Body.Read(chunk)
			if n > 0 {
				frameBuf = append(frameBuf, chunk[:n]...)
				for {
					need, ok := cursorEndpointFrameLen(frameBuf)
					if !ok || len(frameBuf) < need {
						break
					}
					flag := frameBuf[0]
					payload := frameBuf[5:need]
					frameBuf = frameBuf[need:]
					if flag == cursorproto.EnvelopeFlagEndStream {
						var cerr cursorConnectError
						if errJSON := json.Unmarshal(payload, &cerr); errJSON == nil && cerr.Code != "" {
							out <- cursorStreamEvent{Err: &cerr}
						}
						return
					}
					serverMsg, errDecode := cursorproto.DecodeAgentServerMessage(payload)
					if errDecode != nil {
						log.Warnf("cursor run: skip undecodable frame (%d bytes): %v", len(payload), errDecode)
						continue
					}
					// Full-duplex protocol: answer KV blob requests and exec
					// context requests inline, otherwise the server stalls.
					// Hand replies to the writer goroutine — never write
					// synchronously here.
					if reply := t.protocolReply(serverMsg); reply != nil {
						select {
						case replyCh <- cursorproto.EncodeEnvelopeFrame(cursorproto.EnvelopeFlagData, reply.Encode()):
						case <-ctx.Done():
							return
						}
					}
					select {
					case out <- cursorStreamEvent{Message: serverMsg}:
					case <-ctx.Done():
						return
					}
				}
			}
			if errRead != nil {
				if !errors.Is(errRead, io.EOF) {
					select {
					case out <- cursorStreamEvent{Err: fmt.Errorf("cursor run: stream read: %w", errRead)}:
					case <-ctx.Done():
					}
				}
				return
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = resp.Body.Close()
		case <-done:
		}
	}()
	return out, resp.Header.Clone(), nil
}

// FetchUsableModels calls the unary GetUsableModels RPC (application/proto,
// no envelope framing).
func (t *cursorTransport) FetchUsableModels(ctx context.Context, baseURL, token string) ([]cursorproto.ModelDetailsInfo, error) {
	endpoint := strings.TrimSuffix(baseURL, "/") + "/agent.v1.AgentService/GetUsableModels"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	req.Header = t.headers(token, "application/proto")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cursor models: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return cursorproto.DecodeGetUsableModelsResponse(data)
}

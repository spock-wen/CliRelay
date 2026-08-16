package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

// CursorExecutor executes chat requests through Cursor's agent.v1 protocol
// using subscription-account OAuth tokens.
type CursorExecutor struct {
	cfg *config.Config
}

// NewCursorExecutor constructs a Cursor executor.
func NewCursorExecutor(cfg *config.Config) *CursorExecutor {
	return &CursorExecutor{cfg: cfg}
}

func (e *CursorExecutor) Identifier() string { return "cursor" }

func (e *CursorExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	token, _ := cursorCreds(auth)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func (e *CursorExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	client := newCursorTransport(e.cfg, auth).client
	return client.Do(req)
}

// cursorCreds extracts the access token and base URL from an auth record.
func cursorCreds(auth *cliproxyauth.Auth) (token, baseURL string) {
	if auth == nil {
		return "", ""
	}
	meta := auth.Metadata
	if meta != nil {
		if v, ok := meta["access_token"].(string); ok {
			token = v
		}
		if v, ok := meta["base_url"].(string); ok {
			baseURL = v
		}
	}
	if baseURL == "" && auth.Attributes != nil {
		baseURL = auth.Attributes["base_url"]
	}
	if baseURL == "" {
		baseURL = cursorauth.APIBaseURL
	}
	return token, baseURL
}

// cursorUpstreamModel normalizes the relay-side model ID into the bare Cursor
// catalog ID. The request handler strips trailing context-window markers
// ("model[1M]") and routing prefixes before dispatch, but direct API callers
// and prefixed auth entries ("Test/model") can still reach the executor with
// decorations intact; Cursor ignores unknown model IDs and returns an empty
// interaction, so every decoration must be removed here.
func cursorUpstreamModel(model string, authPrefix string) string {
	model = strings.TrimSpace(model)
	if prefix := strings.TrimSpace(authPrefix); prefix != "" {
		if lower, lowerPrefix := strings.ToLower(model), strings.ToLower(prefix)+"/"; strings.HasPrefix(lower, lowerPrefix) {
			model = model[len(prefix)+1:]
		}
	}
	if strings.HasPrefix(strings.ToLower(model), "cursor/") {
		model = model[len("cursor/"):]
	}
	// Defensive: strip a trailing "[1M]"-style context-window marker if the
	// payload model leaked through unnormalized.
	if idx := strings.LastIndex(model, "["); idx > 0 && strings.HasSuffix(model, "]") {
		model = strings.TrimSpace(model[:idx])
	}
	return model
}

func (e *CursorExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	execCtx := newExecutionContext(ctx, e.Identifier(), e.cfg, auth, req, opts, ExecutionOptions{
		TargetFormat: sdktranslator.FromString("claude"),
	})
	reporter := execCtx.Reporter()
	defer reporter.trackFailure(execCtx.Context, &err)

	token, baseURL := cursorCreds(auth)
	if token == "" {
		return resp, fmt.Errorf("cursor executor: auth is missing access token")
	}
	payload := req.Payload
	// Always derive the upstream model from the normalized request model:
	// the raw payload model may carry suffixes ("[1M]") or prefixes that
	// Cursor's catalog does not know.
	model := cursorUpstreamModel(req.Model, authPrefixOf(auth))

	shape, err := parseCursorRequest(payload)
	if err != nil {
		return resp, err
	}
	shape.Model = model

	conversationID := shape.Conversation
	if conversationID == "" {
		conversationID = uuid.NewString()
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	transport := newCursorTransport(e.cfg, auth)
	clientMsg := buildCursorClientMessage(shape, conversationID, transport.session)
	events, _, err := transport.RunStream(runCtx, baseURL, token, clientMsg)
	if err != nil {
		return resp, mapCursorTransportError(err)
	}

	assembler := newCursorAnthropicAssembler(model)
	assembler.ShellToolName = cursorShellToolName(shape.Tools)
	stopReason := "end_turn"
	for {
		ev, ok, quiescent := nextCursorEvent(events, assembler.ToolUseQuiescent())
		if quiescent {
			stopReason = "tool_use"
			break
		}
		if !ok {
			break
		}
		if ev.Err != nil {
			return resp, mapCursorConnectError(ev.Err)
		}
		assembler.Handle(ev.Message)
	}
	resp = cliproxyexecutor.Response{Payload: assembler.BuildNonStreamResponse(stopReason)}
	reporter.publishWithContent(execCtx.Context, cursorUsageDetail(req.Payload, assembler), string(req.Payload), string(resp.Payload))
	return resp, nil
}

// cursorToolQuiescence is how long the executor waits after a tool call
// completes before finalizing the response. Cursor keeps the stream open to
// receive exec results; the window absorbs parallel tool calls that are
// still streaming so they are not truncated.
const cursorToolQuiescence = 500 * time.Millisecond

// nextCursorEvent reads the next stream event. When quiescent is set (a tool
// call has completed), it waits at most cursorToolQuiescence for further
// activity before reporting quiescent=true so the caller can hand the
// tool_use back to the client instead of blocking forever.
func nextCursorEvent(events <-chan cursorStreamEvent, quiescent bool) (ev cursorStreamEvent, ok bool, timedOut bool) {
	if !quiescent {
		ev, ok = <-events
		return ev, ok, false
	}
	timer := time.NewTimer(cursorToolQuiescence)
	defer timer.Stop()
	select {
	case ev, ok = <-events:
		return ev, ok, false
	case <-timer.C:
		return cursorStreamEvent{}, false, true
	}
}

func (e *CursorExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	execCtx := newExecutionContext(ctx, e.Identifier(), e.cfg, auth, req, opts, ExecutionOptions{
		TargetFormat: sdktranslator.FromString("claude"),
	})
	reporter := execCtx.Reporter()
	defer reporter.trackFailure(execCtx.Context, &err)

	token, baseURL := cursorCreds(auth)
	if token == "" {
		return nil, fmt.Errorf("cursor executor: auth is missing access token")
	}
	payload := req.Payload
	model := cursorUpstreamModel(req.Model, authPrefixOf(auth))

	shape, errParse := parseCursorRequest(payload)
	if errParse != nil {
		return nil, errParse
	}
	shape.Model = model

	conversationID := shape.Conversation
	if conversationID == "" {
		conversationID = uuid.NewString()
	}
	transport := newCursorTransport(e.cfg, auth)
	clientMsg := buildCursorClientMessage(shape, conversationID, transport.session)
	events, headers, errRun := transport.RunStream(ctx, baseURL, token, clientMsg)
	if errRun != nil {
		return nil, mapCursorTransportError(errRun)
	}

	reporter.setInputContent(string(payload))
	out := make(chan cliproxyexecutor.StreamChunk, 16)
	go func() {
		defer close(out)
		// send aborts when the consumer disappears (request canceled): an
		// unguarded send would block this goroutine — and the transport
		// resources it holds — until ctx cancellation anyway.
		send := func(chunk cliproxyexecutor.StreamChunk) bool {
			select {
			case out <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		assembler := newCursorAnthropicAssembler(model)
		assembler.ShellToolName = cursorShellToolName(shape.Tools)
		stopReason := "end_turn"
		streamErr := error(nil)
		for {
			ev, ok, quiescent := nextCursorEvent(events, assembler.ToolUseQuiescent())
			if quiescent {
				stopReason = "tool_use"
				break
			}
			if !ok {
				break
			}
			if ev.Err != nil {
				streamErr = mapCursorConnectError(ev.Err)
				send(cliproxyexecutor.StreamChunk{Err: streamErr})
				break
			}
			for _, event := range assembler.Handle(ev.Message) {
				line, errJSON := json.Marshal(event.SSE)
				if errJSON != nil {
					continue
				}
				reporter.appendOutputChunk(line)
				if !send(cliproxyexecutor.StreamChunk{Payload: append(append([]byte("event: "+event.eventName()+"\ndata: "), line...), '\n', '\n')}) {
					return
				}
			}
		}
		if streamErr != nil {
			reporter.publishFailure(execCtx.Context)
			return
		}
		for _, event := range assembler.Finish(stopReason) {
			line, errJSON := json.Marshal(event.SSE)
			if errJSON != nil {
				continue
			}
			reporter.appendOutputChunk(line)
			if !send(cliproxyexecutor.StreamChunk{Payload: append(append([]byte("event: "+event.eventName()+"\ndata: "), line...), '\n', '\n')}) {
				return
			}
		}
		reporter.publish(execCtx.Context, cursorUsageDetail(payload, assembler))
	}()
	return &cliproxyexecutor.StreamResult{Headers: headers, Chunks: out}, nil
}

func (e *CursorExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	var resp cliproxyexecutor.Response
	approx := approximateTokenCount(req.Payload)
	body := map[string]any{"input_tokens": approx}
	out, _ := json.Marshal(body)
	resp = cliproxyexecutor.Response{Payload: out}
	return resp, nil
}

// Refresh exchanges the refresh token for a new access token. Auth records
// imported via dashboard API key (auth_kind=api_key) re-exchange the stored
// key instead: the exchange response's refresh token is single-use-invalid.
func (e *CursorExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("cursor executor: auth is nil")
	}
	var apiKey string
	var refreshToken string
	if auth.Metadata != nil {
		if v, ok := auth.Metadata["api_key"].(string); ok {
			apiKey = strings.TrimSpace(v)
		}
		if v, ok := auth.Metadata["refresh_token"].(string); ok {
			refreshToken = strings.TrimSpace(v)
		}
	}
	if apiKey == "" && refreshToken == "" {
		return auth, nil
	}
	svc := cursorauth.NewAuthWithProxyURL(e.cfg, auth.ProxyURL)
	var pair *cursorauth.TokenPair
	var err error
	authKind := "oauth"
	if apiKey != "" {
		pair, err = svc.ExchangeAPIKey(ctx, apiKey)
		authKind = "api_key"
	} else {
		pair, err = svc.RefreshTokens(ctx, refreshToken)
	}
	if err != nil {
		return nil, err
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["type"] = "cursor"
	auth.Metadata["auth_kind"] = authKind
	auth.Metadata["access_token"] = pair.AccessToken
	if apiKey == "" {
		auth.Metadata["refresh_token"] = pair.RefreshToken
	}
	if subject, userID, email := cursorauth.ParseTokenIdentity(pair.AccessToken); subject != "" {
		auth.Metadata["sub"] = subject
		if userID != "" {
			auth.Metadata["user_id"] = userID
		}
		if email != "" {
			auth.Metadata["email"] = email
		}
	}
	if exp, ok := cursorauth.ParseTokenExpiry(pair.AccessToken); ok {
		auth.Metadata["expired"] = exp.Format(time.RFC3339)
	}
	if strings.TrimSpace(cursorMetaString(auth.Metadata, "base_url")) == "" {
		auth.Metadata["base_url"] = cursorauth.APIBaseURL
	}
	auth.Metadata["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes["auth_kind"] = authKind
	if strings.TrimSpace(auth.Attributes["base_url"]) == "" {
		auth.Attributes["base_url"] = cursorauth.APIBaseURL
	}
	return auth, nil
}

func cursorMetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// mapCursorConnectError converts Connect end-stream errors into status errors
// understood by the credential scheduler.
func mapCursorConnectError(err error) error {
	var cerr *cursorConnectError
	if ce, ok := err.(*cursorConnectError); ok {
		cerr = ce
	} else {
		return err
	}
	switch {
	case cerr.Region():
		// Region-blocked model: permanent for this account; not a cooldown case.
		return statusErr{code: http.StatusForbidden, msg: cerr.Error()}
	case strings.EqualFold(cerr.Code, "resource_exhausted"),
		strings.EqualFold(cerr.Code, "unavailable"):
		return statusErr{code: http.StatusTooManyRequests, msg: cerr.Error()}
	case strings.EqualFold(cerr.Code, "unauthenticated"), strings.EqualFold(cerr.Code, "permission_denied"):
		return statusErr{code: http.StatusUnauthorized, msg: cerr.Error()}
	case strings.EqualFold(cerr.Code, "not_found"):
		return statusErr{code: http.StatusNotFound, msg: cerr.Error()}
	default:
		return statusErr{code: http.StatusBadGateway, msg: cerr.Error()}
	}
}

func mapCursorTransportError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403") {
		return statusErr{code: http.StatusUnauthorized, msg: msg}
	}
	if strings.Contains(msg, "status 429") {
		return statusErr{code: http.StatusTooManyRequests, msg: msg}
	}
	return err
}

// approximateTokenCount estimates tokens from the request payload (~4 chars/token).
func approximateTokenCount(payload []byte) int64 {
	if len(payload) == 0 {
		return 0
	}
	// Prefer explicit input length if the payload carries one.
	if v := gjson.GetBytes(payload, "input_tokens"); v.Exists() {
		return v.Int()
	}
	return int64(len(payload) / 4)
}

// authPrefixOf returns the auth entry's model routing prefix, if any.
func authPrefixOf(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	return strings.TrimSpace(auth.Prefix)
}

// cursorUsageDetail builds a usage record for the request logs. Cursor's
// subscription protocol does not report billed token counts, so input tokens
// are approximated from the payload (same estimate CountTokens uses) and
// output tokens come from the assembler's token_delta accumulation. Without
// non-zero counts the usage pipeline drops the record entirely.
func cursorUsageDetail(payload []byte, assembler *cursorAnthropicAssembler) coreusage.Detail {
	return coreusage.Detail{
		InputTokens:  approximateTokenCount(payload),
		OutputTokens: assembler.totalOutputTokens(),
	}
}

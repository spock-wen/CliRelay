package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/google/uuid"
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

// cursorModelName strips an optional "cursor/" prefix from the model ID.
func cursorModelName(model string) string {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(strings.ToLower(model), "cursor/") {
		return model[len("cursor/"):]
	}
	return model
}

func (e *CursorExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	var resp cliproxyexecutor.Response
	token, baseURL := cursorCreds(auth)
	if token == "" {
		return resp, fmt.Errorf("cursor executor: auth is missing access token")
	}
	payload := req.Payload
	model := cursorModelName(req.Model)

	shape, err := parseCursorRequest(payload)
	if err != nil {
		return resp, err
	}
	if shape.Model == "" || shape.Model == "cursor" {
		payload = cursorEnsureRequestDefaults(payload, model, 4096)
		shape, err = parseCursorRequest(payload)
		if err != nil {
			return resp, err
		}
		shape.Model = model
	}

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
	for ev := range events {
		if ev.Err != nil {
			return resp, mapCursorConnectError(ev.Err)
		}
		assembler.Handle(ev.Message)
	}
	resp = cliproxyexecutor.Response{Payload: assembler.BuildNonStreamResponse("end_turn")}
	return resp, nil
}

func (e *CursorExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	token, baseURL := cursorCreds(auth)
	if token == "" {
		return nil, fmt.Errorf("cursor executor: auth is missing access token")
	}
	payload := req.Payload
	model := cursorModelName(req.Model)

	shape, errParse := parseCursorRequest(payload)
	if errParse != nil {
		return nil, errParse
	}
	if shape.Model == "" || shape.Model == "cursor" {
		payload = cursorEnsureRequestDefaults(payload, model, 4096)
		shape, errParse = parseCursorRequest(payload)
		if errParse != nil {
			return nil, errParse
		}
		shape.Model = model
	}

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

	out := make(chan cliproxyexecutor.StreamChunk, 16)
	go func() {
		defer close(out)
		assembler := newCursorAnthropicAssembler(model)
		for ev := range events {
			if ev.Err != nil {
				out <- cliproxyexecutor.StreamChunk{Err: mapCursorConnectError(ev.Err)}
				return
			}
			for _, event := range assembler.Handle(ev.Message) {
				line, errJSON := json.Marshal(event.SSE)
				if errJSON != nil {
					continue
				}
				out <- cliproxyexecutor.StreamChunk{Payload: append(append([]byte("event: message\ndata: "), line...), '\n', '\n')}
			}
		}
		for _, event := range assembler.Finish("end_turn") {
			line, errJSON := json.Marshal(event.SSE)
			if errJSON != nil {
				continue
			}
			out <- cliproxyexecutor.StreamChunk{Payload: append(append([]byte("event: message\ndata: "), line...), '\n', '\n')}
		}
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

// Refresh exchanges the refresh token for a new access token.
func (e *CursorExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("cursor executor: auth is nil")
	}
	var refreshToken string
	if auth.Metadata != nil {
		if v, ok := auth.Metadata["refresh_token"].(string); ok {
			refreshToken = strings.TrimSpace(v)
		}
	}
	if refreshToken == "" {
		return auth, nil
	}
	svc := cursorauth.NewAuthWithProxyURL(e.cfg, auth.ProxyURL)
	pair, err := svc.RefreshTokens(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["type"] = "cursor"
	auth.Metadata["auth_kind"] = "oauth"
	auth.Metadata["access_token"] = pair.AccessToken
	auth.Metadata["refresh_token"] = pair.RefreshToken
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
	auth.Attributes["auth_kind"] = "oauth"
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

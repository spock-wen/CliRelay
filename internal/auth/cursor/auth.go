package cursor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

// AuthClient performs the Cursor PKCE login poll and token refresh.
type AuthClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewAuth builds a Cursor auth client honoring the configured proxy.
func NewAuth(cfg *config.Config) *AuthClient {
	return NewAuthWithProxyURL(cfg, "")
}

// NewAuthWithProxyURL builds a Cursor auth client routed through the given proxy.
func NewAuthWithProxyURL(cfg *config.Config, proxyURL string) *AuthClient {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	if strings.TrimSpace(proxyURL) != "" {
		transport := &http.Transport{Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(proxyURL)
		}}
		client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	}
	return &AuthClient{
		httpClient: client,
		baseURL:    strings.TrimSuffix(APIBaseURL, "/"),
	}
}

// SetBaseURL overrides the API base URL (used by tests and self-hosted gateways).
func (a *AuthClient) SetBaseURL(base string) {
	a.baseURL = strings.TrimSuffix(base, "/")
}

// PKCECodes holds an S256 verifier/challenge pair.
type PKCECodes struct {
	Verifier  string
	Challenge string
}

// GeneratePKCECodes creates an S256 PKCE pair.
func GeneratePKCECodes() (*PKCECodes, error) {
	buf := make([]byte, 43)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("cursor pkce: generate verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	return &PKCECodes{Verifier: verifier, Challenge: challenge}, nil
}

// LoginParams describes a pending browser login.
type LoginParams struct {
	Verifier  string
	Challenge string
	UUID      string
	LoginURL  string
}

// NewLoginParams generates a fresh PKCE pair and login URL.
func NewLoginParams() (*LoginParams, error) {
	pkce, err := GeneratePKCECodes()
	if err != nil {
		return nil, err
	}
	loginUUID := uuid.NewString()
	query := url.Values{
		"challenge":     {pkce.Challenge},
		"uuid":          {loginUUID},
		"mode":          {"login"},
		"redirectTarget": {"cli"},
	}
	return &LoginParams{
		Verifier:  pkce.Verifier,
		Challenge: pkce.Challenge,
		UUID:      loginUUID,
		LoginURL:  LoginBaseURL + "?" + query.Encode(),
	}, nil
}

// TokenPair is the token pair returned by login poll and refresh.
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// PollLogin polls the auth endpoint until the browser login completes.
// It returns nil,nil while waiting when onWait is provided (unused by default),
// an error on transport failure or timeout, and the token pair on success.
func (a *AuthClient) PollLogin(ctx context.Context, params *LoginParams, onProgress func(attempt int)) (*TokenPair, error) {
	if params == nil || params.UUID == "" || params.Verifier == "" {
		return nil, fmt.Errorf("cursor poll: login params are required")
	}
	interval := PollInitialInterval
	consecutiveErrors := 0
	for attempt := 0; attempt < PollMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		pair, done, err := a.pollOnce(ctx, params)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= PollMaxConsecutiveErrors {
				return nil, fmt.Errorf("cursor poll: too many consecutive errors: %w", err)
			}
			interval = min(time.Duration(float64(interval)*PollBackoffFactor), PollMaxInterval)
			continue
		}
		if done {
			return pair, nil
		}
		consecutiveErrors = 0
		interval = min(time.Duration(float64(interval)*PollBackoffFactor), PollMaxInterval)
		if onProgress != nil {
			onProgress(attempt)
		}
	}
	return nil, fmt.Errorf("cursor poll: timed out waiting for browser login")
}

func (a *AuthClient) pollOnce(ctx context.Context, params *LoginParams) (pair *TokenPair, done bool, err error) {
	endpoint := fmt.Sprintf("%s%s?uuid=%s&verifier=%s", a.baseURL, AuthPollPath,
		url.QueryEscape(params.UUID), url.QueryEscape(params.Verifier))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		body, errRead := io.ReadAll(resp.Body)
		if errRead != nil {
			return nil, false, errRead
		}
		var out TokenPair
		if errJSON := json.Unmarshal(body, &out); errJSON != nil {
			return nil, false, fmt.Errorf("cursor poll: parse response: %w", errJSON)
		}
		if strings.TrimSpace(out.AccessToken) == "" {
			return nil, false, fmt.Errorf("cursor poll: response missing accessToken")
		}
		return &out, true, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, false, fmt.Errorf("cursor poll: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// RefreshTokens exchanges a refresh token for a new token pair.
func (a *AuthClient) RefreshTokens(ctx context.Context, refreshToken string) (*TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("cursor refresh: refresh token is required")
	}
	endpoint := a.baseURL + RefreshPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("cursor refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+refreshToken)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cursor refresh: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("cursor refresh: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cursor refresh: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out TokenPair
	if errJSON := json.Unmarshal(body, &out); errJSON != nil {
		return nil, fmt.Errorf("cursor refresh: parse response: %w", errJSON)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("cursor refresh: response missing accessToken")
	}
	if strings.TrimSpace(out.RefreshToken) == "" {
		out.RefreshToken = refreshToken
	}
	return &out, nil
}

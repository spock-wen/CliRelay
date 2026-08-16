package cursor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGeneratePKCECodesS256(t *testing.T) {
	pkce, err := GeneratePKCECodes()
	if err != nil {
		t.Fatalf("GeneratePKCECodes() error = %v", err)
	}
	if pkce.Verifier == "" || pkce.Challenge == "" {
		t.Fatalf("empty verifier or challenge")
	}
	if strings.ContainsAny(pkce.Verifier, "+/=") || strings.ContainsAny(pkce.Challenge, "+/=") {
		t.Fatalf("PKCE values must be URL-safe without padding")
	}
	// verifier of 43 random bytes encodes to 58 chars (14 groups of 3 + one
// remainder byte yields 56 + 2 chars, no padding)
	if len(pkce.Verifier) != 58 {
		t.Fatalf("verifier length = %d, want 58", len(pkce.Verifier))
	}
}

func TestNewLoginParamsURL(t *testing.T) {
	params, err := NewLoginParams()
	if err != nil {
		t.Fatalf("NewLoginParams() error = %v", err)
	}
	if !strings.HasPrefix(params.LoginURL, LoginBaseURL+"?") {
		t.Fatalf("login URL prefix wrong: %s", params.LoginURL)
	}
	for _, want := range []string{"challenge=", "uuid=" + params.UUID, "mode=login", "redirectTarget=cli"} {
		if !strings.Contains(params.LoginURL, want) {
			t.Fatalf("login URL missing %q: %s", want, params.LoginURL)
		}
	}
	if params.UUID == "" || params.Verifier == "" || params.Challenge == "" {
		t.Fatalf("incomplete login params: %+v", params)
	}
}

func TestPollLoginWaitsOn404ThenSucceeds(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != AuthPollPath {
			http.NotFound(w, r)
			return
		}
		uuidParam := r.URL.Query().Get("uuid")
		verifier := r.URL.Query().Get("verifier")
		if uuidParam != "test-uuid" || verifier != "test-verifier" {
			http.Error(w, "bad params", http.StatusBadRequest)
			return
		}
		polls++
		if polls < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  "access-token",
			"refreshToken": "refresh-token",
		})
	}))
	defer server.Close()

	client := &AuthClient{httpClient: server.Client(), baseURL: server.URL}
	pair, err := client.PollLogin(context.Background(), &LoginParams{UUID: "test-uuid", Verifier: "test-verifier"}, nil)
	if err != nil {
		t.Fatalf("PollLogin() error = %v", err)
	}
	if pair.AccessToken != "access-token" || pair.RefreshToken != "refresh-token" {
		t.Fatalf("token pair = %+v", pair)
	}
	if polls != 3 {
		t.Fatalf("polls = %d, want 3", polls)
	}
}

func TestPollLoginMissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refreshToken":"rt"}`))
	}))
	defer server.Close()

	client := &AuthClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := client.PollLogin(context.Background(), &LoginParams{UUID: "u", Verifier: "v"}, nil)
	if err == nil || !strings.Contains(err.Error(), "missing accessToken") {
		t.Fatalf("expected missing accessToken error, got %v", err)
	}
}

func TestPollLoginConsecutiveErrorAbort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// simulate persistent gateway failure
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := &AuthClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := client.PollLogin(context.Background(), &LoginParams{UUID: "u", Verifier: "v"}, nil)
	if err == nil || !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("expected consecutive error abort, got %v", err)
	}
}

func TestRefreshTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != RefreshPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer old-refresh" {
			http.Error(w, "bad auth header", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  "new-access",
			"refreshToken": "new-refresh",
		})
	}))
	defer server.Close()

	client := &AuthClient{httpClient: server.Client(), baseURL: server.URL}
	pair, err := client.RefreshTokens(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshTokens() error = %v", err)
	}
	if pair.AccessToken != "new-access" || pair.RefreshToken != "new-refresh" {
		t.Fatalf("pair = %+v", pair)
	}
}

func TestRefreshTokensKeepsOldRefreshWhenMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "new-access"})
	}))
	defer server.Close()

	client := &AuthClient{httpClient: server.Client(), baseURL: server.URL}
	pair, err := client.RefreshTokens(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshTokens() error = %v", err)
	}
	if pair.RefreshToken != "old-refresh" {
		t.Fatalf("refresh token = %q, want old-refresh", pair.RefreshToken)
	}
}

// fakeJWT builds an unsigned JWT carrying the given claims.
func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signature := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return fmt.Sprintf("%s.%s.%s", header, base64.RawURLEncoding.EncodeToString(payload), signature)
}

func TestParseTokenIdentity(t *testing.T) {
	token := fakeJWT(t, map[string]any{
		"sub":   "auth0|user_01KJ9RJ5JKMMAK9P5E2BMHSF8W",
		"email": "hihope_007@catxr.top",
		"exp":   float64(1791952757),
	})
	subject, userID, email := ParseTokenIdentity(token)
	if subject != "auth0|user_01KJ9RJ5JKMMAK9P5E2BMHSF8W" {
		t.Fatalf("subject = %q", subject)
	}
	if userID != "user_01KJ9RJ5JKMMAK9P5E2BMHSF8W" {
		t.Fatalf("userID = %q", userID)
	}
	if email != "hihope_007@catxr.top" {
		t.Fatalf("email = %q", email)
	}
}

func TestParseTokenIdentityNoPipe(t *testing.T) {
	token := fakeJWT(t, map[string]any{"sub": "user_plain"})
	_, userID, _ := ParseTokenIdentity(token)
	if userID != "user_plain" {
		t.Fatalf("userID = %q, want user_plain", userID)
	}
}

func TestParseTokenExpiry(t *testing.T) {
	want := time.Unix(1791952757, 0).UTC()
	token := fakeJWT(t, map[string]any{"exp": float64(1791952757)})
	got, ok := ParseTokenExpiry(token)
	if !ok {
		t.Fatalf("expiry not found")
	}
	if !got.Equal(want) {
		t.Fatalf("expiry = %v, want %v", got, want)
	}
}

func TestBuildTokenStorage(t *testing.T) {
	token := fakeJWT(t, map[string]any{
		"sub":   "auth0|user_abc",
		"email": "a@b.c",
		"exp":   float64(1791952757),
	})
	ts := BuildTokenStorage(&TokenPair{AccessToken: token, RefreshToken: "rt"})
	if ts.Email != "a@b.c" || ts.UserID != "user_abc" || ts.RefreshToken != "rt" {
		t.Fatalf("storage = %+v", ts)
	}
	if ts.Expire == "" {
		t.Fatalf("expiry not set")
	}
	if ts.Type != "" || ts.AuthKind != "" {
		t.Fatalf("type fields should be set on save, got %+v", ts)
	}
}

func TestCredentialFileName(t *testing.T) {
	if got := CredentialFileName("a@b.c", ""); got != "cursor-a-b.c.json" {
		t.Fatalf("file name = %q", got)
	}
	if got := CredentialFileName("", "user_x"); got != "cursor-user_x.json" {
		t.Fatalf("file name = %q", got)
	}
	if got := CredentialFileName("", ""); !strings.HasPrefix(got, "cursor-") {
		t.Fatalf("file name = %q", got)
	}
}

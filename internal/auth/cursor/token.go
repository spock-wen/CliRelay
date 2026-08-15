package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	log "github.com/sirupsen/logrus"
)

// TokenStorage persists the Cursor OAuth token pair.
type TokenStorage struct {
	Type          string `json:"type"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token,omitempty"`
	Expire        string `json:"expired,omitempty"`
	LastRefresh   string `json:"last_refresh,omitempty"`
	Email         string `json:"email,omitempty"`
	Subject       string `json:"sub,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	AuthKind      string `json:"auth_kind,omitempty"`

	Metadata map[string]any `json:"-"`
}

// SetMetadata attaches metadata merged into the persisted JSON.
func (ts *TokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

// SaveTokenToFile writes the token storage to disk as JSON.
func (ts *TokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "cursor"
	ts.AuthKind = "oauth"
	if errMkdir := os.MkdirAll(filepath.Dir(authFilePath), 0o700); errMkdir != nil {
		return fmt.Errorf("cursor token storage: create directory: %w", errMkdir)
	}
	file, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("cursor token storage: create token file: %w", err)
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			log.Errorf("cursor token storage: close token file error: %v", errClose)
		}
	}()
	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("cursor token storage: merge metadata: %w", errMerge)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(data); err != nil {
		return fmt.Errorf("cursor token storage: write token file: %w", err)
	}
	return nil
}

// CredentialFileName derives the auth file name from the account identity.
func CredentialFileName(email, userID string) string {
	email = sanitizeFileSegment(email)
	if email != "" {
		return fmt.Sprintf("cursor-%s.json", email)
	}
	userID = sanitizeFileSegment(userID)
	if userID != "" {
		return fmt.Sprintf("cursor-%s.json", userID)
	}
	return fmt.Sprintf("cursor-%d.json", time.Now().UnixMilli())
}

func sanitizeFileSegment(value string) string {
	value = strings.TrimSpace(value)
	replaced := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, value)
	return strings.Trim(replaced, "-")
}

// jwtClaims decodes the payload segment of a JWT without signature verification.
func jwtClaims(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, false
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if errJSON := json.Unmarshal(raw, &claims); errJSON != nil {
		return nil, false
	}
	return claims, true
}

// ParseTokenIdentity extracts the account subject and email from the access token.
// Cursor subjects look like "auth0|user_xxx"; the user segment is the API user ID.
func ParseTokenIdentity(accessToken string) (subject string, userID string, email string) {
	claims, ok := jwtClaims(accessToken)
	if !ok {
		return "", "", ""
	}
	if v, ok := claims["sub"].(string); ok {
		subject = strings.TrimSpace(v)
		if idx := strings.LastIndex(subject, "|"); idx >= 0 && idx+1 < len(subject) {
			userID = subject[idx+1:]
		} else {
			userID = subject
		}
	}
	if v, ok := claims["email"].(string); ok {
		email = strings.TrimSpace(v)
	}
	return subject, userID, email
}

// ParseTokenExpiry returns the access token expiration from its exp claim.
func ParseTokenExpiry(accessToken string) (time.Time, bool) {
	claims, ok := jwtClaims(accessToken)
	if !ok {
		return time.Time{}, false
	}
	exp, ok := claims["exp"].(float64)
	if !ok || exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0).UTC(), true
}

// BuildTokenStorage converts a token pair into persisted storage, deriving
// identity and expiry from the access token JWT.
func BuildTokenStorage(pair *TokenPair) *TokenStorage {
	if pair == nil {
		return nil
	}
	subject, userID, email := ParseTokenIdentity(pair.AccessToken)
	ts := &TokenStorage{
		AccessToken:  strings.TrimSpace(pair.AccessToken),
		RefreshToken: strings.TrimSpace(pair.RefreshToken),
		Subject:      subject,
		UserID:       userID,
		Email:        email,
		BaseURL:      APIBaseURL,
	}
	if exp, ok := ParseTokenExpiry(pair.AccessToken); ok {
		ts.Expire = exp.Format(time.RFC3339)
	}
	return ts
}

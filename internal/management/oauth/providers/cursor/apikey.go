// Package cursor implements the management-UI API-key import flow for Cursor
// subscription accounts. A dashboard key ("crsr_...") exchanges at
// /auth/exchange_user_api_key for a short-lived session token pair; the key
// itself is persisted in metadata so the auto-refresh service can re-exchange
// it on schedule (the exchanged refresh token is single-use-invalid).
package cursor

import (
	"context"
	"fmt"
	"strings"
	"time"

	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

// APIKeyLoginOptions carries the import flow dependencies.
type APIKeyLoginOptions struct {
	Ctx        context.Context
	SaveRecord func(context.Context, *coreauth.Auth) (string, error)
}

// APIKeyLoginResult reports the persisted auth file path and account identity.
type APIKeyLoginResult struct {
	SavedPath string
	Email     string
	UserID    string
	Expired   string
}

// AuthenticateAPIKey exchanges a dashboard key and persists an auth record
// with auth_kind=api_key. The stored metadata keeps the original key plus the
// exchanged tokens; the executor's Refresh re-exchanges the key when the
// session token approaches expiry.
func AuthenticateAPIKey(opts APIKeyLoginOptions, apiKey string) (*APIKeyLoginResult, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, fmt.Errorf("cursor api key: key is required")
	}
	if !strings.HasPrefix(key, "crsr_") {
		return nil, fmt.Errorf("cursor api key: unexpected key format (want crsr_... prefix)")
	}
	if opts.SaveRecord == nil {
		return nil, fmt.Errorf("cursor api key: save record callback is required")
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	exchangeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	authClient := cursorauth.NewAuth(nil)
	pair, err := authClient.ExchangeAPIKey(exchangeCtx, key)
	if err != nil {
		return nil, err
	}

	subject, userID, email := cursorauth.ParseTokenIdentity(pair.AccessToken)
	fileName := cursorauth.CredentialFileName(email, userID)
	label := email
	if label == "" {
		label = "Cursor"
	}
	expired := ""
	if exp, ok := cursorauth.ParseTokenExpiry(pair.AccessToken); ok {
		expired = exp.Format(time.RFC3339)
	}
	record := &coreauth.Auth{
		ID:       fileName,
		Provider: "cursor",
		FileName: fileName,
		Label:    label,
		Metadata: map[string]any{
			"type":         "cursor",
			"auth_kind":    "api_key",
			"api_key":      key,
			"access_token": pair.AccessToken,
			"expired":      expired,
			"sub":          subject,
			"user_id":      userID,
			"base_url":     cursorauth.APIBaseURL,
			"last_refresh": time.Now().UTC().Format(time.RFC3339),
		},
		Attributes: map[string]string{
			"auth_kind": "api_key",
			"base_url":  cursorauth.APIBaseURL,
		},
	}
	if strings.TrimSpace(pair.RefreshToken) != "" {
		record.Metadata["refresh_token"] = pair.RefreshToken
	}

	savedPath, errSave := opts.SaveRecord(ctx, record)
	if errSave != nil {
		return nil, fmt.Errorf("cursor api key: save record: %w", errSave)
	}
	return &APIKeyLoginResult{
		SavedPath: savedPath,
		Email:     email,
		UserID:    userID,
		Expired:   expired,
	}, nil
}

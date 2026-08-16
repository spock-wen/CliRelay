package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// CursorAuthenticator implements the browser PKCE poll login flow for Cursor
// subscription accounts. Unlike OAuth providers with a localhost callback,
// Cursor login completes out of band: the user authorizes in the browser and
// the CLI polls api2.cursor.sh/auth/poll until tokens appear.
type CursorAuthenticator struct{}

// NewCursorAuthenticator constructs a Cursor authenticator.
func NewCursorAuthenticator() Authenticator {
	return &CursorAuthenticator{}
}

func (CursorAuthenticator) Provider() string {
	return "cursor"
}

func (CursorAuthenticator) RefreshLead() *time.Duration {
	lead := cursorauth.RefreshLead()
	return &lead
}

func (a CursorAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	authSvc := cursorauth.NewAuth(cfg)
	params, err := cursorauth.NewLoginParams()
	if err != nil {
		return nil, fmt.Errorf("cursor: generate login params: %w", err)
	}

	fmt.Println("Opening browser for Cursor authentication")
	if !opts.NoBrowser {
		if !browser.IsAvailable() {
			log.Warn("No browser available; please open the URL manually")
			fmt.Printf("Visit the following URL to continue authentication:\n%s\n", params.LoginURL)
		} else if errOpen := browser.OpenURL(params.LoginURL); errOpen != nil {
			log.Warnf("Failed to open browser automatically: %v", errOpen)
			fmt.Printf("Visit the following URL to continue authentication:\n%s\n", params.LoginURL)
		}
	} else {
		fmt.Printf("Visit the following URL to continue authentication:\n%s\n", params.LoginURL)
	}

	fmt.Println("Waiting for Cursor authentication...")
	loginCtx, cancel := context.WithTimeout(ctx, cursorauth.LoginTimeout)
	defer cancel()

	pair, err := authSvc.PollLogin(loginCtx, params, nil)
	if err != nil {
		return nil, fmt.Errorf("cursor: %w", err)
	}

	tokenStorage := cursorauth.BuildTokenStorage(pair)
	if tokenStorage == nil || strings.TrimSpace(tokenStorage.AccessToken) == "" {
		return nil, fmt.Errorf("cursor token storage missing access token")
	}

	fileName := cursorauth.CredentialFileName(tokenStorage.Email, tokenStorage.UserID)
	label := strings.TrimSpace(tokenStorage.Email)
	if label == "" {
		label = "Cursor"
	}

	metadata := map[string]any{
		"type":          "cursor",
		"access_token":  tokenStorage.AccessToken,
		"refresh_token": tokenStorage.RefreshToken,
		"expired":       tokenStorage.Expire,
		"base_url":      tokenStorage.BaseURL,
		"auth_kind":     "oauth",
	}
	if tokenStorage.Email != "" {
		metadata["email"] = tokenStorage.Email
	}
	if tokenStorage.Subject != "" {
		metadata["sub"] = tokenStorage.Subject
	}
	if tokenStorage.UserID != "" {
		metadata["user_id"] = tokenStorage.UserID
	}

	fmt.Println("Cursor authentication successful")

	return &coreauth.Auth{
		ID:       fileName,
		Provider: a.Provider(),
		FileName: fileName,
		Label:    label,
		Storage:  tokenStorage,
		Metadata: metadata,
		Attributes: map[string]string{
			"auth_kind": "oauth",
			"base_url":  tokenStorage.BaseURL,
		},
	}, nil
}

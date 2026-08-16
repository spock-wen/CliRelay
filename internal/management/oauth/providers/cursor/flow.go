// Package cursor implements the management-UI OAuth login flow for Cursor
// subscription accounts. Unlike callback-based providers, Cursor completes
// login out of band: the user authorizes in the browser and this flow polls
// api2.cursor.sh/auth/poll until the token pair appears.
package cursor

import (
	"context"
	"fmt"
	"time"

	cursorauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cursor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

// OAuthLoginOptions carries the flow dependencies.
type OAuthLoginOptions struct {
	Ctx        context.Context
	Sessions   SessionCallbacks
	SaveRecord func(context.Context, *coreauth.Auth) (string, error)
	OnProgress func(string)
	OnAuthURL  func(string)
}

// SessionCallbacks tracks login session state for the management UI.
type SessionCallbacks struct {
	Register         func(state, provider string)
	SetError         func(state, message string)
	Complete         func(state string)
	CompleteProvider func(provider string) int
}

// OAuthLoginResult reports the login URL immediately; tokens arrive via the
// background poll.
type OAuthLoginResult struct {
	LoginURL string
	State    string
}

// StartOAuthLogin begins the browser login flow. It returns the login URL for
// the UI to open and completes asynchronously: the token record is saved (or
// the session errored) when polling finishes.
func StartOAuthLogin(opts OAuthLoginOptions) (*OAuthLoginResult, error) {
	params, err := cursorauth.NewLoginParams()
	if err != nil {
		return nil, fmt.Errorf("cursor: generate login params: %w", err)
	}

	state := params.UUID
	if opts.Sessions.Register != nil {
		opts.Sessions.Register(state, "cursor")
	}
	if opts.OnAuthURL != nil {
		opts.OnAuthURL(params.LoginURL)
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		authClient := cursorauth.NewAuth(nil)
		pollCtx, cancel := context.WithTimeout(ctx, cursorauth.LoginTimeout)
		defer cancel()
		pair, errPoll := authClient.PollLogin(pollCtx, params, nil)
		if errPoll != nil {
			if opts.Sessions.SetError != nil {
				opts.Sessions.SetError(state, errPoll.Error())
			}
			return
		}
		tokenStorage := cursorauth.BuildTokenStorage(pair)
		if tokenStorage == nil || tokenStorage.AccessToken == "" {
			if opts.Sessions.SetError != nil {
				opts.Sessions.SetError(state, "cursor: token storage missing access token")
			}
			return
		}
		fileName := cursorauth.CredentialFileName(tokenStorage.Email, tokenStorage.UserID)
		label := tokenStorage.Email
		if label == "" {
			label = "Cursor"
		}
		record := &coreauth.Auth{
			ID:       fileName,
			Provider: "cursor",
			FileName: fileName,
			Label:    label,
			Storage:  tokenStorage,
			Metadata: map[string]any{
				"type":          "cursor",
				"access_token":  tokenStorage.AccessToken,
				"refresh_token": tokenStorage.RefreshToken,
				"expired":       tokenStorage.Expire,
				"email":         tokenStorage.Email,
				"sub":           tokenStorage.Subject,
				"user_id":       tokenStorage.UserID,
				"base_url":      tokenStorage.BaseURL,
				"auth_kind":     "oauth",
			},
			Attributes: map[string]string{
				"auth_kind": "oauth",
				"base_url":  tokenStorage.BaseURL,
			},
		}
		if opts.SaveRecord != nil {
			saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer saveCancel()
			if _, errSave := opts.SaveRecord(saveCtx, record); errSave != nil {
				if opts.Sessions.SetError != nil {
					opts.Sessions.SetError(state, errSave.Error())
				}
				return
			}
		}
		if opts.Sessions.Complete != nil {
			opts.Sessions.Complete(state)
		}
		if opts.Sessions.CompleteProvider != nil {
			opts.Sessions.CompleteProvider("cursor")
		}
	}()

	return &OAuthLoginResult{LoginURL: params.LoginURL, State: state}, nil
}

// Package cursor provides authentication helpers for Cursor subscription accounts.
package cursor

import "time"

const (
	// LoginBaseURL is the browser login entry that initiates the PKCE handshake.
	LoginBaseURL = "https://cursor.com/loginDeepControl"
	// APIBaseURL is the Cursor backend serving the agent protocol and auth endpoints.
	APIBaseURL = "https://api2.cursor.sh"
	// AuthPollPath is polled until the browser login completes.
	AuthPollPath = "/auth/poll"
	// RefreshPath exchanges a refresh token for a new token pair.
	RefreshPath = "/auth/exchange_user_api_key"
	// UsagePath reports subscription usage for the bearer token.
	UsagePath = "/auth/usage"

	// ClientVersion identifies this client to the Cursor backend. The backend
	// gates protocol behaviour on this value; it must track the current Cursor
	// CLI release. Override via config when the backend rejects it.
	ClientVersion = "cli-2026.07.23-e383d2b"
	// ClientType marks requests as CLI traffic.
	ClientType = "cli"

	// PollMaxAttempts bounds the login polling loop.
	PollMaxAttempts = 150
	// PollInitialInterval is the delay before the first poll.
	PollInitialInterval = time.Second
	// PollMaxInterval caps the exponential backoff between polls.
	PollMaxInterval = 10 * time.Second
	// PollBackoffFactor grows the interval between polls.
	PollBackoffFactor = 1.2
	// PollMaxConsecutiveErrors aborts after repeated network failures.
	PollMaxConsecutiveErrors = 3
	// LoginTimeout bounds the whole login flow.
	LoginTimeout = 10 * time.Minute
)

// refreshLead is how long before token expiry a refresh is triggered.
var refreshLead = 5 * time.Minute

// RefreshLead returns the duration before token expiry when refresh should occur.
func RefreshLead() time.Duration { return refreshLead }

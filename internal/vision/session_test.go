package vision

import (
	"net/http"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestResolveIsolatedSessionKey(t *testing.T) {
	opts := cliproxyexecutor.Options{
		Headers: http.Header{"Session-Id": []string{"sess-1"}},
	}
	auth := &cliproxyauth.Auth{ID: "user-A"}

	key, ok := ResolveIsolatedSessionKey(opts, auth)
	if !ok {
		t.Fatal("expected ok")
	}
	if want := SessionKey("user-A::sess-1"); key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}

func TestResolveIsolatedSessionKeySameSessionDifferentUsers(t *testing.T) {
	mkOpts := func(sid string) cliproxyexecutor.Options {
		return cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{sid}}}
	}
	a := &cliproxyauth.Auth{ID: "user-A"}
	b := &cliproxyauth.Auth{ID: "user-B"}
	kA, _ := ResolveIsolatedSessionKey(mkOpts("shared"), a)
	kB, _ := ResolveIsolatedSessionKey(mkOpts("shared"), b)
	if kA == kB {
		t.Fatalf("same session id across users collided: %q == %q", kA, kB)
	}
}

func TestResolveIsolatedSessionKeySameUserDifferentSessions(t *testing.T) {
	mkOpts := func(sid string) cliproxyexecutor.Options {
		return cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{sid}}}
	}
	u := &cliproxyauth.Auth{ID: "user-A"}
	k1, _ := ResolveIsolatedSessionKey(mkOpts("s1"), u)
	k2, _ := ResolveIsolatedSessionKey(mkOpts("s2"), u)
	if k1 == k2 {
		t.Fatalf("same user sessions collided: %q == %q", k1, k2)
	}
}

func TestResolveIsolatedSessionKeyNilAuth(t *testing.T) {
	opts := cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{"sess-1"}}}
	if _, ok := ResolveIsolatedSessionKey(opts, nil); ok {
		t.Fatal("expected !ok for nil auth")
	}
}

func TestResolveIsolatedSessionKeyEmptyAuthID(t *testing.T) {
	opts := cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{"sess-1"}}}
	if _, ok := ResolveIsolatedSessionKey(opts, &cliproxyauth.Auth{ID: ""}); ok {
		t.Fatal("expected !ok for empty auth ID")
	}
}

func TestResolveIsolatedSessionKeyNoSession(t *testing.T) {
	if _, ok := ResolveIsolatedSessionKey(cliproxyexecutor.Options{}, &cliproxyauth.Auth{ID: "user-A"}); ok {
		t.Fatal("expected !ok when no session id present")
	}
}

package auth

import "testing"

// TestResetCooldownCountsOnlyActualResets ensures a misspelled model name on a
// matched channel is not reported as a successful reset.
func TestResetCooldownCountsOnlyActualResets(t *testing.T) {
	mgr := &Manager{
		auths: map[string]*Auth{
			"a1": {
				ID:    "a1",
				Label: "ch-1",
				ModelStates: map[string]*ModelState{
					"model-a": {Unavailable: true, Status: StatusError},
					"model-b": {Unavailable: true, Status: StatusError},
				},
			},
		},
	}

	t.Run("misspelled model reports zero", func(t *testing.T) {
		if got := mgr.ResetCooldown("ch-1", "model-does-not-exist"); got != 0 {
			t.Fatalf("ResetCooldown(misspelled) = %d, want 0", got)
		}
	})

	t.Run("exact model reports one", func(t *testing.T) {
		if got := mgr.ResetCooldown("ch-1", "model-a"); got != 1 {
			t.Fatalf("ResetCooldown(exact) = %d, want 1", got)
		}
	})

	t.Run("no model resets one channel", func(t *testing.T) {
		if got := mgr.ResetCooldown("ch-1", ""); got != 1 {
			t.Fatalf("ResetCooldown(all) = %d, want 1", got)
		}
	})

	t.Run("unknown channel reports zero", func(t *testing.T) {
		if got := mgr.ResetCooldown("no-such-channel", ""); got != 0 {
			t.Fatalf("ResetCooldown(unknown channel) = %d, want 0", got)
		}
	})
}

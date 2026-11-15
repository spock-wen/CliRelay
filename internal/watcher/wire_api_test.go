package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/watcher/diff"
)

func TestReloadWireAPIResponses(t *testing.T) {
	dir := t.TempDir()
	w := &Watcher{configPath: filepath.Join(dir, "config.yaml"), authDir: dir, mirroredAuthDir: dir}
	w.SetConfig(&config.Config{AuthDir: dir})
	queue := make(chan AuthUpdate, 4)
	w.SetAuthUpdateQueue(queue)
	t.Cleanup(func() { w.SetAuthUpdateQueue(nil) })

	body := func(field string) string {
		return "auth-dir: " + dir + "\nopenai-compatibility:\n  - name: GPT\n    base-url: http://127.0.0.1:8317/v1\n    api-key-entries:\n      - api-key: test-key\n" + field
	}
	var authID string
	for i, field := range []string{"", "    wire-api: responses\n", ""} {
		previous := w.config
		if err := os.WriteFile(w.configPath, []byte(body(field)), 0o600); err != nil {
			t.Fatal(err)
		}
		if !w.reloadConfig() {
			t.Fatal("reloadConfig failed")
		}
		select {
		case update := <-queue:
			if update.Auth == nil {
				t.Fatalf("step %d: %#v", i, update)
			}
			if i == 0 {
				authID = update.Auth.ID
			} else if update.Action != AuthUpdateActionModify || update.Auth.ID != authID {
				t.Fatalf("step %d: expected the same credential to be modified, got %#v", i, update)
			}
			want := ""
			if i == 1 {
				want = config.OpenAICompatWireAPIResponses
			}
			if got := update.Auth.Attributes["wire_api"]; got != want {
				t.Fatalf("step %d: wire_api = %q, want %q", i, got, want)
			}
			if i > 0 {
				changes := strings.Join(diff.BuildConfigChangeDetails(previous, w.config), "\n")
				if !strings.Contains(changes, "wire-api") {
					t.Fatalf("step %d: reload summary missing wire-api:\n%s", i, changes)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("step %d: no auth update", i)
		}
	}
}

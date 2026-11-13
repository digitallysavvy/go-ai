package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TS readCodexSubscription (codex-subscription.ts 136, 303-312): CODEX_HOME
// is read with `??`, so a set but empty value resolves (path.resolve of an empty string) to
// the working directory; toCodexCredential accepts any string refresh token,
// "" included.
func TestR90toolsCodexHomeEmptyAndEmptyRefreshToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("CODEX_HOME", "")
	accessToken := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	data, _ := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": accessToken, "refresh_token": ""},
	})
	if err := os.WriteFile(filepath.Join(cwd, "auth.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	env, ok := readCodexSubscription(context.Background())
	if !ok || env["CODEX_API_KEY"] != accessToken {
		t.Fatalf("credential in the working directory with an empty refresh token not used: ok=%v env=%v", ok, env)
	}
}

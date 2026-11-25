package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TS rewrites the credential file from the parsed object spread, so every key
// keeps its position (no alphabetical re-sorting) and keys the refresh does
// not know about survive.
func TestRefreshedCredentialFileKeepsKeyOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		// No refresh_token: the stored one is kept (TS `refreshed.refreshToken ?? refreshToken`).
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "expires_in": 3600})
	}))
	t.Cleanup(srv.Close)
	old := claudeTokenURL
	claudeTokenURL = srv.URL
	t.Cleanup(func() { claudeTokenURL = old })

	soon := time.Now().Add(time.Minute).UnixMilli()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".credentials.json")
	src := fmt.Sprintf(`{"zeta":1,"claudeAiOauth":{"scopes":["a"],"refreshToken":"keep-me","accessToken":"old","expiresAt":%d,"subscriptionType":"max"},"alpha":true}`, soon)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readClaudeCodeSubscription(context.Background()); !ok {
		t.Fatal("expected a refreshed credential")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		OAuth struct {
			ExpiresAt int64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.OAuth.ExpiresAt <= soon {
		t.Fatalf("expiresAt not refreshed: %v %d", err, parsed.OAuth.ExpiresAt)
	}
	want := fmt.Sprintf("{\n  \"zeta\": 1,\n  \"claudeAiOauth\": {\n    \"scopes\": [\n      \"a\"\n    ],\n    \"refreshToken\": \"keep-me\",\n    \"accessToken\": \"new-access\",\n    \"expiresAt\": %d,\n    \"subscriptionType\": \"max\"\n  },\n  \"alpha\": true\n}\n", parsed.OAuth.ExpiresAt)
	if string(raw) != want {
		t.Fatalf("file =\n%s\nwant\n%s", raw, want)
	}
}

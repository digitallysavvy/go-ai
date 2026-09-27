package claudecode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TS claude-code-subscription.test.ts: no credential file present.
func TestReadClaudeCodeSubscription_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if _, ok := readClaudeCodeSubscription(context.Background()); ok {
		t.Fatal("expected (_, false) when no credential file exists")
	}
}

// A malformed credential file (no claudeAiOauth) is treated as "not found".
func TestReadClaudeCodeSubscription_MissingOAuthField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeCredentialFile(t, home, map[string]any{"somethingElse": true})
	if _, ok := readClaudeCodeSubscription(context.Background()); ok {
		t.Fatal("expected (_, false) for a file without claudeAiOauth")
	}
}

// A non-expiring credential is returned as-is; no refresh, no file rewrite.
func TestReadClaudeCodeSubscription_ValidNonExpiring(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	farFuture := time.Now().Add(24 * time.Hour).UnixMilli()
	path := writeCredentialFile(t, home, map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": "access-1", "refreshToken": "refresh-1", "expiresAt": float64(farFuture),
		},
	})

	env, ok := readClaudeCodeSubscription(context.Background())
	if !ok {
		t.Fatal("expected a usable credential")
	}
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "access-1" {
		t.Errorf("CLAUDE_CODE_OAUTH_TOKEN = %q, want access-1", env["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if env["ANTHROPIC_BASE_URL"] != claudeSubscriptionBaseURL {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want %q", env["ANTHROPIC_BASE_URL"], claudeSubscriptionBaseURL)
	}

	// The file must be untouched (no refresh attempted).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	oauth := store["claudeAiOauth"].(map[string]any)
	if oauth["accessToken"] != "access-1" {
		t.Errorf("file accessToken = %v, want unchanged access-1", oauth["accessToken"])
	}
}

// CLAUDE_CONFIG_DIR overrides the default `~/.claude` location.
func TestReadClaudeCodeSubscription_ConfigDirOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // must NOT be used
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	farFuture := time.Now().Add(24 * time.Hour).UnixMilli()
	if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), mustJSON(t, map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": "override-token", "refreshToken": "r", "expiresAt": float64(farFuture),
		},
	}), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	env, ok := readClaudeCodeSubscription(context.Background())
	if !ok {
		t.Fatal("expected a usable credential under CLAUDE_CONFIG_DIR")
	}
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "override-token" {
		t.Errorf("CLAUDE_CODE_OAUTH_TOKEN = %q, want override-token", env["CLAUDE_CODE_OAUTH_TOKEN"])
	}
}

// An expiring credential is refreshed and written back to disk.
func TestReadClaudeCodeSubscription_RefreshesExpiringToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-access", "refresh_token": "refreshed-refresh", "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)
	oldTokenURL := claudeTokenURL
	claudeTokenURL = srv.URL
	t.Cleanup(func() { claudeTokenURL = oldTokenURL })

	soon := time.Now().Add(1 * time.Minute).UnixMilli() // within the 5-minute default refresh window
	path := writeCredentialFile(t, home, map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": "stale-access", "refreshToken": "stale-refresh", "expiresAt": float64(soon),
		},
	})

	env, ok := readClaudeCodeSubscription(context.Background())
	if !ok {
		t.Fatal("expected a usable (refreshed) credential")
	}
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "refreshed-access" {
		t.Errorf("CLAUDE_CODE_OAUTH_TOKEN = %q, want refreshed-access", env["CLAUDE_CODE_OAUTH_TOKEN"])
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	oauth := store["claudeAiOauth"].(map[string]any)
	if oauth["accessToken"] != "refreshed-access" {
		t.Errorf("file accessToken = %v, want refreshed-access", oauth["accessToken"])
	}
	if oauth["refreshToken"] != "refreshed-refresh" {
		t.Errorf("file refreshToken = %v, want refreshed-refresh", oauth["refreshToken"])
	}
}

func writeCredentialFile(t *testing.T, home string, value map[string]any) string {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, mustJSON(t, value), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

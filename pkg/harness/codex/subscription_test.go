package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeJWT builds a minimal (unsigned) JWT with the given `exp` claim (epoch
// seconds), matching what subscription.GetJWTExpiresAt decodes.
func fakeJWT(t *testing.T, expUnixSeconds int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]any{"exp": expUnixSeconds})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// TS codex-subscription: no auth.json present.
func TestReadCodexSubscription_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	if _, ok := readCodexSubscription(context.Background()); ok {
		t.Fatal("expected (_, false) when no auth.json exists")
	}
}

// auth_mode other than "chatgpt" is not a usable subscription credential.
func TestReadCodexSubscription_WrongAuthMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	writeAuthFile(t, home, map[string]any{
		"auth_mode": "apikey",
		"tokens":    map[string]any{"access_token": fakeJWT(t, time.Now().Add(24*time.Hour).Unix()), "refresh_token": "r"},
	})
	if _, ok := readCodexSubscription(context.Background()); ok {
		t.Fatal("expected (_, false) for auth_mode != chatgpt")
	}
}

// A non-expiring ChatGPT credential is returned as-is; no refresh, no file
// rewrite.
func TestReadCodexSubscription_ValidNonExpiring(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	accessToken := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	path := writeAuthFile(t, home, map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": accessToken, "refresh_token": "refresh-1", "account_id": "acct-1"},
	})

	env, ok := readCodexSubscription(context.Background())
	if !ok {
		t.Fatal("expected a usable credential")
	}
	if env["CODEX_API_KEY"] != accessToken {
		t.Errorf("CODEX_API_KEY mismatch")
	}
	if env["OPENAI_BASE_URL"] != chatGPTCodexBaseURL {
		t.Errorf("OPENAI_BASE_URL = %q, want %q", env["OPENAI_BASE_URL"], chatGPTCodexBaseURL)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	tokens := store["tokens"].(map[string]any)
	if tokens["access_token"] != accessToken {
		t.Errorf("file access_token changed unexpectedly")
	}
}

// CODEX_HOME overrides the default `~/.codex` location.
func TestReadCodexSubscription_CodexHomeOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // must NOT be used
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	accessToken := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	data, err := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": accessToken, "refresh_token": "r"},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	env, ok := readCodexSubscription(context.Background())
	if !ok {
		t.Fatal("expected a usable credential under CODEX_HOME")
	}
	if env["CODEX_API_KEY"] != accessToken {
		t.Errorf("CODEX_API_KEY mismatch")
	}
}

// An expiring credential is refreshed and written back to disk.
func TestReadCodexSubscription_RefreshesExpiringToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")

	refreshedAccessToken := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": refreshedAccessToken, "refresh_token": "refreshed-refresh", "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)
	oldTokenURL := openaiTokenURL
	openaiTokenURL = srv.URL
	t.Cleanup(func() { openaiTokenURL = oldTokenURL })

	staleAccessToken := fakeJWT(t, time.Now().Add(1*time.Minute).Unix()) // within the 5-minute default refresh window
	path := writeAuthFile(t, home, map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": staleAccessToken, "refresh_token": "stale-refresh"},
	})

	env, ok := readCodexSubscription(context.Background())
	if !ok {
		t.Fatal("expected a usable (refreshed) credential")
	}
	if env["CODEX_API_KEY"] != refreshedAccessToken {
		t.Errorf("CODEX_API_KEY = %q, want the refreshed token", env["CODEX_API_KEY"])
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	tokens := store["tokens"].(map[string]any)
	if tokens["access_token"] != refreshedAccessToken {
		t.Errorf("file access_token = %v, want the refreshed token", tokens["access_token"])
	}
	if tokens["refresh_token"] != "refreshed-refresh" {
		t.Errorf("file refresh_token = %v, want refreshed-refresh", tokens["refresh_token"])
	}
	if store["last_refresh"] == nil {
		t.Error("file last_refresh not set after a refresh")
	}
}

func writeAuthFile(t *testing.T, home string, value map[string]any) string {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

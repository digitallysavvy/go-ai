package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
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

type fakeKeyring struct {
	store   map[string]string
	writes  int
	lastAcc string
}

func (k *fakeKeyring) Read(_ context.Context, service, account string) string {
	k.lastAcc = account
	return k.store[service+"|"+account]
}

func (k *fakeKeyring) Write(_ context.Context, service, account, value string) error {
	k.writes++
	k.store[service+"|"+account] = value
	return nil
}

func useFakeKeyring(t *testing.T, k *fakeKeyring) {
	t.Helper()
	old := newCodexKeyring
	newCodexKeyring = func() codexKeyring { return k }
	t.Cleanup(func() { newCodexKeyring = old })
}

func chatgptAuth(t *testing.T, access string) map[string]any {
	return map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": access, "refresh_token": "r", "account_id": "acct-9"},
	}
}

// TS readCodexSubscription: `cli_auth_credentials_store = "keyring"` reads the
// OS keyring entry for the canonical CODEX_HOME, and returns the ChatGPT
// account id as a request header.
func TestReadCodexSubscription_KeyringStore(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", codexHome)
	access := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	data, _ := json.Marshal(chatgptAuth(t, access))
	k := &fakeKeyring{store: map[string]string{}}
	useFakeKeyring(t, k)
	// Discover the account name the reader uses, then seed it.
	readCodexSubscriptionWithMode(context.Background(), "keyring")
	k.store["Codex Auth|"+k.lastAcc] = string(data)
	if len(k.lastAcc) != len("cli|")+16 {
		t.Fatalf("account = %q", k.lastAcc)
	}

	sub, ok := readCodexSubscriptionWithMode(context.Background(), "keyring")
	if !ok || sub.Environment["CODEX_API_KEY"] != access {
		t.Fatalf("keyring credential not used: %+v ok=%v", sub, ok)
	}
	if sub.RequestHeaders["ChatGPT-Account-ID"] != "acct-9" {
		t.Fatalf("request headers = %v", sub.RequestHeaders)
	}
}

// keyring mode never falls back to the file; file mode never reads the keyring.
func TestReadCodexSubscription_StoreModeIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	access := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	writeAuthFile(t, home, chatgptAuth(t, access))
	useFakeKeyring(t, &fakeKeyring{store: map[string]string{}})

	if _, ok := readCodexSubscriptionWithMode(context.Background(), "keyring"); ok {
		t.Fatal("keyring mode must not read auth.json")
	}
	if _, ok := readCodexSubscriptionWithMode(context.Background(), "auto"); !ok {
		t.Fatal("auto mode falls back to auth.json")
	}
	if _, ok := readCodexSubscriptionWithMode(context.Background(), "ephemeral"); ok {
		t.Fatal("ephemeral mode has no stored credential")
	}
}

// The store mode comes from config.toml when not overridden.
func TestReadCodexSubscription_ConfigTomlStoreMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	access := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	writeAuthFile(t, home, chatgptAuth(t, access))
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(cfg, []byte("model = \"x\"\ncli_auth_credentials_store = 'ephemeral' # note\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCodexSubscription(context.Background()); ok {
		t.Fatal("ephemeral from config.toml must disable stored credentials")
	}
	// Keys after a table header are not top-level.
	if err := os.WriteFile(cfg, []byte("[profile]\ncli_auth_credentials_store = \"ephemeral\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCodexSubscription(context.Background()); !ok {
		t.Fatal("table-scoped key must be ignored")
	}
}

// The account id header is merged into the credential transformation.
func TestRequestTransformations_AccountIDHeader(t *testing.T) {
	env := map[string]string{"CODEX_API_KEY": "real", "OPENAI_BASE_URL": chatGPTCodexBaseURL}
	sandbox := map[string]string{"CODEX_API_KEY": "placeholder", "OPENAI_BASE_URL": chatGPTCodexBaseURL}
	ts, err := requestTransformations(env, sandbox, AuthModeDirect, map[string]string{"ChatGPT-Account-ID": "acct-9"})
	if err != nil || len(ts) != 1 {
		t.Fatalf("ts=%v err=%v", ts, err)
	}
	h := ts[0].Transform.Headers
	if h["ChatGPT-Account-ID"] != "acct-9" || h["Authorization"] != "Bearer real" {
		t.Fatalf("headers = %v", h)
	}
}

// A refreshed credential is written back to the store it was read from.
func TestReadCodexSubscription_KeyringRefreshWritesBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	refreshed := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": refreshed, "refresh_token": "r2", "expires_in": 3600})
	}))
	t.Cleanup(srv.Close)
	old := openaiTokenURL
	openaiTokenURL = srv.URL
	t.Cleanup(func() { openaiTokenURL = old })

	k := &fakeKeyring{store: map[string]string{}}
	useFakeKeyring(t, k)
	readCodexSubscriptionWithMode(context.Background(), "keyring")
	stale := fakeJWT(t, time.Now().Add(time.Minute).Unix())
	data, _ := json.Marshal(chatgptAuth(t, stale))
	k.store["Codex Auth|"+k.lastAcc] = string(data)

	sub, ok := readCodexSubscriptionWithMode(context.Background(), "keyring")
	if !ok || sub.Environment["CODEX_API_KEY"] != refreshed {
		t.Fatalf("not refreshed: %+v", sub)
	}
	if k.writes != 1 || !strings.Contains(k.store["Codex Auth|"+k.lastAcc], refreshed) {
		t.Fatalf("keyring write-back missing: writes=%d", k.writes)
	}
}

// TS lets a failed OAuth refresh propagate out of resolveCodexAuthentication
// (and so out of doStart) instead of treating it as "no credential".
func TestResolveAuthentication_RefreshFailureFailsStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	for _, k := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN"} {
		t.Setenv(k, "")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	old := openaiTokenURL
	openaiTokenURL = srv.URL
	t.Cleanup(func() { openaiTokenURL = old })
	writeAuthFile(t, home, chatgptAuth(t, fakeJWT(t, time.Now().Add(time.Minute).Unix())))

	_, _, err := resolveAuthentication(context.Background(), harness.Authentication{}, map[string]string{}, "")
	if err == nil {
		t.Fatal("expected the refresh failure to be returned")
	}
}

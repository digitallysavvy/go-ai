package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func r87provaJWT(exp time.Time) string {
	payload, _ := json.Marshal(map[string]interface{}{"exp": exp.Unix()})
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

// r87provaOIDCEnv isolates the Vercel CLI data directory and the working
// directory, and links a project.
func r87provaOIDCEnv(t *testing.T, linked bool) string {
	t.Helper()
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	work := t.TempDir()
	if linked {
		if err := os.MkdirAll(filepath.Join(work, ".vercel"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, ".vercel", "project.json"), []byte(`{"projectId":"prj_1","orgId":"team_1"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(work)
	return data
}

func r87provaWriteCLIAuth(t *testing.T, data, body string) {
	t.Helper()
	dir := filepath.Join(data, "com.vercel.cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TS @vercel/oidc getVercelOidcToken: a valid token is used as is; a missing
// or expired one is refreshed through the Vercel API with the CLI login, in
// memory, without touching the CLI files.
func TestR87provaVercelOIDCTokenRefresh(t *testing.T) {
	data := r87provaOIDCEnv(t, true)
	r87provaWriteCLIAuth(t, data, fmt.Sprintf(`{"token":"cli-token","expiresAt":%d}`, time.Now().Add(time.Hour).Unix()))
	authBefore, _ := os.ReadFile(filepath.Join(data, "com.vercel.cli", "auth.json"))

	fresh := r87provaJWT(time.Now().Add(time.Hour))
	var requests atomic.Int32
	var gotURL, gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		gotURL, gotAuth = r.Method+" "+r.URL.String(), r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": fresh})
	}))
	defer api.Close()

	fn := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL})

	valid := r87provaJWT(time.Now().Add(time.Hour))
	t.Setenv("VERCEL_OIDC_TOKEN", valid)
	if tok, err := fn(context.Background()); err != nil || tok != valid || requests.Load() != 0 {
		t.Fatalf("valid token: %q, %v, %d requests", tok, err, requests.Load())
	}

	t.Setenv("VERCEL_OIDC_TOKEN", r87provaJWT(time.Now().Add(-time.Minute)))
	tok, err := fn(context.Background())
	if err != nil || tok != fresh {
		t.Fatalf("refresh: %q, %v", tok, err)
	}
	if gotURL != "POST /v1/projects/prj_1/token?source=vercel-oidc-refresh&teamId=team_1" || gotAuth != "Bearer cli-token" {
		t.Fatalf("refresh request = %s %s", gotURL, gotAuth)
	}
	// The refreshed token is reused from memory; nothing is written.
	if tok, _ := fn(context.Background()); tok != fresh || requests.Load() != 1 {
		t.Fatalf("second call: %q, %d requests", tok, requests.Load())
	}
	if after, _ := os.ReadFile(filepath.Join(data, "com.vercel.cli", "auth.json")); string(after) != string(authBefore) {
		t.Fatal("CLI auth file changed")
	}
	if _, err := os.Stat(filepath.Join(data, "com.vercel.token")); !os.IsNotExist(err) {
		t.Fatal("token cache written")
	}
	if os.Getenv("VERCEL_OIDC_TOKEN") == fresh {
		t.Fatal("process environment changed")
	}

	// A request-context token wins over the environment.
	ctxTok := r87provaJWT(time.Now().Add(time.Hour))
	if tok, _ := fn(ContextWithVercelOIDCToken(context.Background(), ctxTok)); tok != ctxTok {
		t.Fatalf("context token = %q", tok)
	}

	// ExpirationBuffer refreshes a token that expires soon.
	soon := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL, ExpirationBuffer: 2 * time.Hour, AccessToken: func(context.Context) (string, error) { return "explicit", nil }})
	t.Setenv("VERCEL_OIDC_TOKEN", valid)
	if _, err := soon(context.Background()); err != nil || gotAuth != "Bearer explicit" {
		t.Fatalf("buffer refresh: %v, %s", err, gotAuth)
	}
}

func TestR87provaVercelOIDCTokenErrors(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer api.Close()

	t.Run("not linked", func(t *testing.T) {
		r87provaOIDCEnv(t, false)
		_, err := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL})(context.Background())
		var oidcErr *VercelOIDCTokenError
		if !errors.As(err, &oidcErr) || err.Error() != vercelOIDCMissingMessage+"\nUnable to find project root directory. Have you linked your project with `vc link?`" {
			t.Fatalf("err = %q", err)
		}
	})
	t.Run("no CLI login", func(t *testing.T) {
		r87provaOIDCEnv(t, true)
		_, err := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL})(context.Background())
		if err == nil || !strings.HasSuffix(err.Error(), "\nNo authentication found. Please log in with the Vercel CLI (vercel login).") {
			t.Fatalf("err = %q", err)
		}
	})
	t.Run("expired CLI login is not refreshed or cleared", func(t *testing.T) {
		data := r87provaOIDCEnv(t, true)
		body := `{"token":"cli-token","expiresAt":1,"refreshToken":"r"}`
		r87provaWriteCLIAuth(t, data, body)
		_, err := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL})(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Failed to refresh authentication token.") {
			t.Fatalf("err = %q", err)
		}
		if after, _ := os.ReadFile(filepath.Join(data, "com.vercel.cli", "auth.json")); string(after) != body {
			t.Fatal("CLI auth file changed")
		}
	})
	t.Run("API failure", func(t *testing.T) {
		r87provaOIDCEnv(t, true)
		_, err := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL, AccessToken: func(context.Context) (string, error) { return "a", nil }})(context.Background())
		if err == nil || !strings.HasSuffix(err.Error(), "\nFailed to refresh OIDC token: Forbidden") {
			t.Fatalf("err = %q", err)
		}
	})
	t.Run("malformed token", func(t *testing.T) {
		r87provaOIDCEnv(t, true)
		t.Setenv("VERCEL_OIDC_TOKEN", "not-a-jwt")
		_, err := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL})(context.Background())
		if err == nil || err.Error() != "\nInvalid token. Please run `vc env pull` and try again" {
			t.Fatalf("err = %q", err)
		}
	})
	t.Run("cached project token is read", func(t *testing.T) {
		data := r87provaOIDCEnv(t, true)
		cached := r87provaJWT(time.Now().Add(time.Hour))
		_ = os.MkdirAll(filepath.Join(data, "com.vercel.token"), 0o755)
		_ = os.WriteFile(filepath.Join(data, "com.vercel.token", "prj_1.json"), []byte(`{"token":"`+cached+`"}`), 0o600)
		tok, err := VercelOIDCTokenFunc(VercelOIDCTokenOptions{APIBaseURL: api.URL})(context.Background())
		if err != nil || tok != cached {
			t.Fatalf("cached: %q, %v", tok, err)
		}
	})
}

// Config.OIDCTokenFunc is used when no API key is configured; its error is
// the cause of the contextual GatewayAuthenticationError.
func TestR87provaGatewayOIDCTokenFunc(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	tok, method, err := resolveGatewayAuthToken(context.Background(), Config{OIDCTokenFunc: func(context.Context) (string, error) { return "oidc-token", nil }})
	if err != nil || tok != "oidc-token" || method != "oidc" {
		t.Fatalf("%q %q %v", tok, method, err)
	}
	cause := errors.New("boom")
	_, _, err = resolveGatewayAuthToken(context.Background(), Config{OIDCTokenFunc: func(context.Context) (string, error) { return "", cause }})
	var authErr *gatewayerrors.GatewayAuthenticationError
	if !errors.As(err, &authErr) || !errors.Is(err, cause) {
		t.Fatalf("err = %v", err)
	}
	// An API key wins and the func is not called.
	called := false
	tok, method, _ = resolveGatewayAuthToken(context.Background(), Config{APIKey: "k", OIDCTokenFunc: func(context.Context) (string, error) { called = true; return "x", nil }})
	if tok != "k" || method != "api-key" || called {
		t.Fatalf("%q %q %v", tok, method, called)
	}
}

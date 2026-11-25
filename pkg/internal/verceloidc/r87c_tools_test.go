package verceloidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Round 87c (V061 49): @vercel/oidc 4.0.0, the version @vercel/sandbox 3.9.0
// pins, against 3.2.0, the version TS @ai-sdk/gateway pins. Expected values
// are from the 4.0.0 test file (dist/get-vercel-oidc-token-with-refresh.test.js)
// and from node runs of the 4.0.0 and 3.2.0 getVercelOidcToken (scratch
// harness, @vercel/cli-config stubbed to the file credential storage the TS
// tests mock).

func r87ctoolsJWT(payload map[string]any) string {
	b, _ := json.Marshal(payload)
	return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
}

func r87ctoolsRawJWT(payload string) string {
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
}

// r87ctoolsEnv isolates the Vercel data directory and the working directory
// (not linked) and clears the token variables; it returns the data dir.
func r87ctoolsEnv(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv(TokenFileEnv, "")
	t.Chdir(t.TempDir())
	return data
}

type r87ctoolsRequests struct {
	mu   sync.Mutex
	seen []string
}

func (r *r87ctoolsRequests) server(t *testing.T, token string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.seen = append(r.seen, req.Method+" "+req.URL.RequestURI()+" "+req.Header.Get("Authorization"))
		r.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (r *r87ctoolsRequests) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func r87ctoolsWriteFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// get-vercel-oidc-token-with-refresh.test.js "reads the Vercel OIDC token file
// on each call after rotation", "prefers the advertised file over a static
// environment token", "keeps the request token ahead of the advertised file",
// "includes the file path when the Vercel OIDC token file is empty",
// "rejects a missing advertised token file instead of using the environment
// token".
func TestR87ctoolsV4TokenFile(t *testing.T) {
	future := time.Now().Add(12 * time.Hour).Unix()
	first := r87ctoolsJWT(map[string]any{"sub": "test-sub", "exp": future, "v": "first"})
	second := r87ctoolsJWT(map[string]any{"sub": "test-sub", "exp": future, "v": "second"})

	t.Run("rotation", func(t *testing.T) {
		r87ctoolsEnv(t)
		file := filepath.Join(t.TempDir(), "oidc-token")
		t.Setenv(TokenFileEnv, file)
		r := New(Options{V4: true})
		r87ctoolsWriteFile(t, file, first+"\n")
		if got, err := r.Token(context.Background()); err != nil || got != first {
			t.Fatalf("first = %q, %v", got, err)
		}
		r87ctoolsWriteFile(t, file, second+"\n")
		if got, err := r.Token(context.Background()); err != nil || got != second {
			t.Fatalf("second = %q, %v", got, err)
		}
	})

	t.Run("file over environment", func(t *testing.T) {
		r87ctoolsEnv(t)
		t.Setenv("VERCEL_OIDC_TOKEN", r87ctoolsJWT(map[string]any{"exp": future, "v": "stale"}))
		file := filepath.Join(t.TempDir(), "oidc-token")
		t.Setenv(TokenFileEnv, file)
		r87ctoolsWriteFile(t, file, second)
		var reqs r87ctoolsRequests
		r := New(Options{V4: true, APIBaseURL: reqs.server(t, "unused")})
		got, err := r.TokenFor(context.Background(), "prj_test123", "team_test456", time.Minute)
		if err != nil || got != second {
			t.Fatalf("token = %q, %v", got, err)
		}
		if n := len(reqs.all()); n != 0 {
			t.Fatalf("%d refresh requests, want none", n)
		}
	})

	t.Run("request token ahead of file", func(t *testing.T) {
		r87ctoolsEnv(t)
		t.Setenv(TokenFileEnv, filepath.Join(t.TempDir(), "missing-token"))
		request := r87ctoolsJWT(map[string]any{"exp": future, "v": "request"})
		got, err := New(Options{V4: true}).TokenFor(ContextWithToken(context.Background(), request), "prj_test123", "team_test456", time.Minute)
		if err != nil || got != request {
			t.Fatalf("token = %q, %v", got, err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		r87ctoolsEnv(t)
		file := filepath.Join(t.TempDir(), "oidc-token")
		t.Setenv(TokenFileEnv, file)
		r87ctoolsWriteFile(t, file, " \n")
		_, err := New(Options{V4: true}).Token(context.Background())
		var tokenErr *TokenError
		if !errors.As(err, &tokenErr) || err.Error() != "The Vercel OIDC token file is empty: "+file {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		r87ctoolsEnv(t)
		t.Setenv("VERCEL_OIDC_TOKEN", first)
		t.Setenv(TokenFileEnv, filepath.Join(t.TempDir(), "missing-token"))
		// TS rejects with the fs ENOENT error itself (Go: the os error).
		if _, err := New(Options{V4: true}).Token(context.Background()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("err = %v, want the missing file error", err)
		}
	})

	t.Run("BOM and whitespace trimmed", func(t *testing.T) {
		r87ctoolsEnv(t)
		file := filepath.Join(t.TempDir(), "oidc-token")
		t.Setenv(TokenFileEnv, file)
		r87ctoolsWriteFile(t, file, "\uFEFF "+first+"\n")
		if got, err := New(Options{V4: true}).Token(context.Background()); err != nil || got != first {
			t.Fatalf("token = %q, %v", got, err)
		}
	})

	t.Run("3.2.0 ignores the file", func(t *testing.T) {
		r87ctoolsEnv(t)
		file := filepath.Join(t.TempDir(), "oidc-token")
		t.Setenv(TokenFileEnv, file)
		r87ctoolsWriteFile(t, file, first)
		t.Setenv("VERCEL_OIDC_TOKEN", second)
		if got, err := New(Options{}).Token(context.Background()); err != nil || got != second {
			t.Fatalf("token = %q, %v", got, err)
		}
	})
}

// "refreshes an expired file token using the provided team and project", and
// the 4.0.0 saveToken / loadToken round trip: the second lookup of the still
// expired file token reuses the refreshed project token without a request.
func TestR87ctoolsV4ExpiredFileTokenRefresh(t *testing.T) {
	data := r87ctoolsEnv(t)
	file := filepath.Join(t.TempDir(), "oidc-token")
	t.Setenv(TokenFileEnv, file)
	r87ctoolsWriteFile(t, file, r87ctoolsJWT(map[string]any{"sub": "test-sub", "exp": 1, "iat": 1}))
	r87ctoolsWriteFile(t, filepath.Join(data, "com.vercel.cli", "auth.json"), `{"token":"test-auth-token"}`)
	refreshed := r87ctoolsJWT(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "v": "refreshed"})
	var reqs r87ctoolsRequests
	r := New(Options{V4: true, APIBaseURL: reqs.server(t, refreshed)})
	for i := 0; i < 2; i++ {
		got, err := r.TokenFor(context.Background(), "prj_test123", "team_test456", 0)
		if err != nil || got != refreshed {
			t.Fatalf("call %d: token = %q, %v", i, got, err)
		}
	}
	want := []string{"POST /v1/projects/prj_test123/token?source=vercel-oidc-refresh&teamId=team_test456 Bearer test-auth-token"}
	if got := reqs.all(); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("requests = %q, want %q", got, want)
	}
}

// Error texts (node runs of 4.0.0 and 3.2.0 getVercelOidcToken), and isExpired
// on a null payload and on exp values that JS coerces with ToNumber.
func TestR87ctoolsErrorTextsAndExpiry(t *testing.T) {
	const unlinked = "Unable to find project root directory. Have you linked your project with `vc link?`"
	cases := []struct {
		name, env string
		v4, v3    string // "" = the token is returned
	}{
		{"missing", "",
			MissingMessageV4 + "\n" + unlinked,
			MissingMessage + "\n" + unlinked},
		{"invalid", "not-a-jwt-token", "\nInvalid token.", "\nInvalid token. Please run `vc env pull` and try again"},
		{"null payload", r87ctoolsRawJWT("null"), "\nCannot read properties of null (reading 'exp')", "\nCannot read properties of null (reading 'exp')"},
		{`exp "1"`, r87ctoolsRawJWT(`{"exp":"1"}`), "\n" + unlinked, "\n" + unlinked},
		{"exp true", r87ctoolsRawJWT(`{"exp":true}`), "\n" + unlinked, "\n" + unlinked},
		{"exp []", r87ctoolsRawJWT(`{"exp":[]}`), "\n" + unlinked, "\n" + unlinked},
		{"exp null", r87ctoolsRawJWT(`{"exp":null}`), "\n" + unlinked, "\n" + unlinked},
		{`exp "abc"`, r87ctoolsRawJWT(`{"exp":"abc"}`), "", ""},
		{"exp {}", r87ctoolsRawJWT(`{"exp":{}}`), "", ""},
		{"exp [1,2]", r87ctoolsRawJWT(`{"exp":[1,2]}`), "", ""},
		{"no exp", r87ctoolsRawJWT(`{}`), "", ""},
		{"number payload", r87ctoolsRawJWT(`5`), "", ""},
	}
	for _, tc := range cases {
		for _, v4 := range []bool{true, false} {
			want := tc.v3
			if v4 {
				want = tc.v4
			}
			r87ctoolsEnv(t)
			t.Setenv("VERCEL_OIDC_TOKEN", tc.env)
			got, err := New(Options{V4: v4}).Token(context.Background())
			if want == "" {
				if err != nil || got != tc.env {
					t.Errorf("%s v4=%v: token = %q, %v", tc.name, v4, got, err)
				}
				continue
			}
			if err == nil || err.Error() != want {
				t.Errorf("%s v4=%v: err = %v, want %q", tc.name, v4, err, want)
			}
		}
	}
}

// 4.0.0 getVercelToken: a CLI login with only a refresh token is refreshed
// (Go: not ported, the RefreshAccessTokenFailedError text), not reported as
// missing as in 3.2.0; neither token nor refresh token is missing in both.
func TestR87ctoolsV4CLILoginRefreshTokenOnly(t *testing.T) {
	expired := r87ctoolsJWT(map[string]any{"exp": 1})
	for _, tc := range []struct {
		auth string
		v4   bool
		want string
	}{
		{`{"refreshToken":"r"}`, true, "\nFailed to refresh authentication token."},
		{`{"refreshToken":"r"}`, false, "\nNo authentication found. Please log in with the Vercel CLI (vercel login)."},
		{`{}`, true, "\nNo authentication found. Please log in with the Vercel CLI (vercel login)."},
		{`{}`, false, "\nNo authentication found. Please log in with the Vercel CLI (vercel login)."},
	} {
		data := r87ctoolsEnv(t)
		t.Setenv("VERCEL_OIDC_TOKEN", expired)
		r87ctoolsWriteFile(t, filepath.Join(data, "com.vercel.cli", "auth.json"), tc.auth)
		_, err := New(Options{V4: tc.v4}).TokenFor(context.Background(), "prj_1", "team_1", 0)
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%s v4=%v: err = %v, want prefix %q", tc.auth, tc.v4, err, tc.want)
		}
	}
}

// 4.0.0 assertVercelOidcTokenResponse texts (3.2.0 keeps the `vc env pull`
// suffix).
func TestR87ctoolsMalformedTokenResponseTexts(t *testing.T) {
	for _, tc := range []struct {
		v4         bool
		value      any
		wantSuffix string
	}{
		{true, "x", "Expected an object."},
		{true, map[string]any{"token": 1.0}, "Expected a string-valued token property."},
		{false, "x", "Expected an object. Please run `vc env pull` and try again"},
		{false, map[string]any{}, "Expected a string-valued token property. Please run `vc env pull` and try again"},
	} {
		_, err := New(Options{V4: tc.v4}).assertTokenResponse(tc.value)
		if err == nil || err.Error() != "Vercel OIDC token is malformed. "+tc.wantSuffix {
			t.Errorf("v4=%v %v: err = %v", tc.v4, tc.value, err)
		}
	}
}

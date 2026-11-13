package vercel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

const r87btoolsSandboxJSON = `{"sandbox":{"name":"sb","persistent":true},"session":{"id":"sess","status":"%s","cwd":"/vercel/sandbox","timeout":%d},"routes":[]}`

func r87btoolsInt64(v int64) *int64 { return &v }

// V061 item 39: @vercel/sandbox 3.9.0 `Sandbox.update` extends a running
// session by `params.timeout - session.timeout` (POST .../extend-timeout
// {"duration"}) before the network policy update and the PATCH; no extension
// for a smaller timeout or a session that is not running; a 410 or a 422
// sandbox_stopping response is ignored, any other error fails the update
// before the PATCH (sandbox.js 1093-1100, session.js 552-558,
// api-client.js 598-605).
func TestR87btoolsUpdateExtendsRunningSessionTimeout(t *testing.T) {
	type step struct{ method, path, body string }
	cases := []struct {
		name        string
		status      string
		timeout     int64
		newTimeout  int64
		policy      bool
		extendReply func(w http.ResponseWriter)
		wantErr     bool
		want        []step
	}{
		{name: "grows", status: "running", timeout: 60000, newTimeout: 90000, policy: true,
			want: []step{
				{"GET", "/v2/sandboxes/sb", ""},
				{"POST", "/v2/sandboxes/sessions/sess/extend-timeout", `{"duration":30000}`},
				{"POST", "/v2/sandboxes/sessions/sess/network-policy", `{"mode":"deny-all"}`},
				{"PATCH", "/v2/sandboxes/sb", `{"timeout":90000,"networkPolicy":{"mode":"deny-all"}}`},
			}},
		{name: "same", status: "running", timeout: 90000, newTimeout: 90000,
			want: []step{{"GET", "/v2/sandboxes/sb", ""}, {"PATCH", "/v2/sandboxes/sb", `{"timeout":90000}`}}},
		{name: "not running", status: "stopped", timeout: 60000, newTimeout: 90000,
			want: []step{{"GET", "/v2/sandboxes/sb", ""}, {"PATCH", "/v2/sandboxes/sb", `{"timeout":90000}`}}},
		{name: "stopped 410", status: "running", timeout: 60000, newTimeout: 90000,
			extendReply: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusGone)
				_, _ = io.WriteString(w, `{"error":{"code":"sandbox_stopped"}}`)
			},
			want: []step{{"GET", "/v2/sandboxes/sb", ""}, {"POST", "/v2/sandboxes/sessions/sess/extend-timeout", `{"duration":30000}`}, {"PATCH", "/v2/sandboxes/sb", `{"timeout":90000}`}}},
		{name: "stopping 422", status: "running", timeout: 60000, newTimeout: 90000,
			extendReply: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"error":{"code":"sandbox_stopping"}}`)
			},
			want: []step{{"GET", "/v2/sandboxes/sb", ""}, {"POST", "/v2/sandboxes/sessions/sess/extend-timeout", `{"duration":30000}`}, {"PATCH", "/v2/sandboxes/sb", `{"timeout":90000}`}}},
		{name: "other 422", status: "running", timeout: 60000, newTimeout: 90000, wantErr: true,
			extendReply: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"error":{"code":"sandbox_snapshotting"}}`)
			},
			want: []step{{"GET", "/v2/sandboxes/sb", ""}, {"POST", "/v2/sandboxes/sessions/sess/extend-timeout", `{"duration":30000}`}}},
	}
	for _, tc := range cases {
		client, reqs, mu := r87toolsWireServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
			switch {
			case r.Method == http.MethodGet:
				_, _ = fmt.Fprintf(w, r87btoolsSandboxJSON, tc.status, tc.timeout)
			case strings.HasSuffix(r.URL.Path, "/extend-timeout"):
				if tc.extendReply != nil {
					tc.extendReply(w)
					return
				}
				_, _ = io.WriteString(w, `{"session":{"id":"sess","status":"running","timeout":90000}}`)
			case strings.HasSuffix(r.URL.Path, "/network-policy"):
				_, _ = io.WriteString(w, `{"session":{"id":"sess","status":"running","timeout":90000,"networkPolicy":{"mode":"deny-all"}}}`)
			default:
				_, _ = io.WriteString(w, `{"sandbox":{"name":"sb","persistent":true}}`)
			}
		})
		sb, err := GetSandbox(context.Background(), client, "sb", nil)
		if err != nil {
			t.Fatal(err)
		}
		params := UpdateParams{TimeoutMs: r87btoolsInt64(tc.newTimeout)}
		if tc.policy {
			deny := DenyAllNetworkPolicy()
			params.NetworkPolicy = &deny
		}
		err = sb.Update(context.Background(), params)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
		mu.Lock()
		got := *reqs
		if len(got) != len(tc.want) {
			t.Errorf("%s: requests = %+v", tc.name, got)
		} else {
			for i, w := range tc.want {
				if got[i].method != w.method || got[i].path != w.path || strings.TrimSpace(got[i].body) != w.body {
					t.Errorf("%s: request %d = %+v, want %+v", tc.name, i, got[i], w)
				}
			}
		}
		mu.Unlock()
		if tc.name == "grows" && sb.sessionTimeout != 90000 {
			t.Errorf("session timeout after extend = %d, want 90000", sb.sessionTimeout)
		}
	}
}

func r87btoolsJWT(owner, project string, exp time.Time) string {
	payload, _ := json.Marshal(map[string]any{"owner_id": owner, "project_id": project, "exp": exp.Unix()})
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

// r87btoolsOIDCEnv isolates the Vercel data directory (XDG_DATA_HOME) and
// the working directory; cached, when set, is the cached project token for
// prj_1 (com.vercel.token/prj_1.json, read only by the refresh).
func r87btoolsOIDCEnv(t *testing.T, envToken, cached string, linked bool) {
	t.Helper()
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("VERCEL_OIDC_TOKEN", envToken)
	t.Setenv("VERCEL_URL", "")
	if cached != "" {
		dir := filepath.Join(data, "com.vercel.token")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prj_1.json"), []byte(`{"token":"`+cached+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
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
}

type r87btoolsAuthSeen struct {
	mu   sync.Mutex
	seen []string // "Authorization teamId"
}

func (s *r87btoolsAuthSeen) server(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, r.Header.Get("Authorization")+" "+r.URL.Query().Get("teamId"))
		s.mu.Unlock()
		_, _ = fmt.Fprintf(w, r87btoolsSandboxJSON, "running", 60000)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (s *r87btoolsAuthSeen) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

// V061 item 38: @vercel/sandbox 3.9.0 APIClient.ensureValidToken (api-client.js
// 102-116) refreshes a JWT token expiring within 5 minutes through
// getVercelOidcToken({expirationBufferMs: 300e3, team, project}) before each
// request and takes the team from the fresh token's owner_id; a failed
// refresh is ignored. Opt-in with OIDCRefresh; without it the token is sent
// as read.
func TestR87btoolsOIDCRefreshBeforeRequests(t *testing.T) {
	expiring := r87btoolsJWT("team_1", "prj_1", time.Now().Add(2*time.Minute))
	fresh := r87btoolsJWT("team_2", "prj_1", time.Now().Add(time.Hour))

	t.Run("refreshes", func(t *testing.T) {
		r87btoolsOIDCEnv(t, expiring, fresh, false)
		var seen r87btoolsAuthSeen
		base := seen.server(t)
		refresh := &OIDCRefresh{}
		if _, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{BaseURL: base, SandboxID: "sb", OIDCRefresh: refresh}); err != nil {
			t.Fatal(err)
		}
		got := seen.all()
		if len(got) != 1 || got[0] != "Bearer "+fresh+" team_2" {
			t.Fatalf("requests = %q, want the refreshed token and its team", got)
		}
		// The refreshed token is kept in the OIDCRefresh (in memory only).
		if os.Getenv("VERCEL_OIDC_TOKEN") != expiring {
			t.Fatal("VERCEL_OIDC_TOKEN was rewritten")
		}
	})

	t.Run("failed refresh keeps the token", func(t *testing.T) {
		r87btoolsOIDCEnv(t, expiring, "", false)
		var seen r87btoolsAuthSeen
		base := seen.server(t)
		refresh := &OIDCRefresh{AccessToken: func(context.Context) (string, error) { return "", errors.New("no login") }}
		if _, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{BaseURL: base, SandboxID: "sb", OIDCRefresh: refresh}); err != nil {
			t.Fatal(err)
		}
		if got := seen.all(); len(got) != 1 || got[0] != "Bearer "+expiring+" team_1" {
			t.Fatalf("requests = %q", got)
		}
	})

	t.Run("not opted in", func(t *testing.T) {
		r87btoolsOIDCEnv(t, expiring, fresh, false)
		var seen r87btoolsAuthSeen
		base := seen.server(t)
		if _, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{BaseURL: base, SandboxID: "sb"}); err != nil {
			t.Fatal(err)
		}
		if got := seen.all(); len(got) != 1 || got[0] != "Bearer "+expiring+" team_1" {
			t.Fatalf("requests = %q", got)
		}
	})
}

// 3.9.0 getCredentials -> getVercelToken: with no explicit credentials and
// no VERCEL_OIDC_TOKEN, getVercelOidcToken refreshes from the linked project
// (here the cached project token); a failure is a LocalOidcContextError,
// which the harness reports as an authentication failure.
func TestR87btoolsOIDCRefreshResolvesMissingToken(t *testing.T) {
	fresh := r87btoolsJWT("team_1", "prj_1", time.Now().Add(time.Hour))
	r87btoolsOIDCEnv(t, "", fresh, true)
	var seen r87btoolsAuthSeen
	base := seen.server(t)
	if _, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{BaseURL: base, SandboxID: "sb", OIDCRefresh: &OIDCRefresh{}}); err != nil {
		t.Fatal(err)
	}
	if got := seen.all(); len(got) != 1 || got[0] != "Bearer "+fresh+" team_1" {
		t.Fatalf("requests = %q", got)
	}

	r87btoolsOIDCEnv(t, "", "", false)
	_, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{BaseURL: base, SandboxID: "sb", OIDCRefresh: &OIDCRefresh{}})
	var authErr *harness.SandboxAuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %v, want a SandboxAuthenticationError", err)
	}
	var ctxErr *vercelOIDCContextError
	if !errors.As(err, &ctxErr) || !strings.HasPrefix(ctxErr.Error(), "Could not get credentials from OIDC context.\nPlease link your Vercel project using `npx vercel link`.") {
		t.Fatalf("cause = %v, want the LocalOidcContextError text", err)
	}
}

// 3.9.0 APIClient constructor: a JWT token with an owner_id claim sets the
// team from its claims, also for explicit credentials (the project stays).
func TestR87btoolsJWTClaimsSetTeamAndProject(t *testing.T) {
	token := r87btoolsJWT("team_claims", "prj_claims", time.Now().Add(time.Hour))
	c := NewAPIClient("http://127.0.0.1", Credentials{Token: token, TeamID: "team_x", ProjectID: "prj_x"})
	// TS keeps credentials.projectId for request bodies and queries; only the
	// teamId query follows the claims (api-client.js 93-100, sandbox.js 350-358).
	if c.credentials.TeamID != "team_claims" || c.credentials.ProjectID != "prj_x" {
		t.Fatalf("credentials = %+v", c.credentials)
	}
	c = NewAPIClient("http://127.0.0.1", Credentials{Token: "plain", TeamID: "team_x", ProjectID: "prj_x"})
	if c.credentials.TeamID != "team_x" || c.credentials.ProjectID != "prj_x" {
		t.Fatalf("credentials = %+v", c.credentials)
	}
}

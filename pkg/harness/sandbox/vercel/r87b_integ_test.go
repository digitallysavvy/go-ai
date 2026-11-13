package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Integrator check of tools affeb762: the 3.9.0 APIClient constructor
// (api-client.js 89-100) sets only `this.teamId` from a JWT token's owner_id;
// the claims' project_id feeds ensureValidToken alone. Sandbox.create passes
// `projectId: credentials.projectId` (sandbox.js 350-358), so explicit
// credentials keep their project on the wire while the teamId query follows
// the token.
func TestR87bintegJWTClaimsTeamOnlyOnTheWire(t *testing.T) {
	var gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sandbox":{"name":"s"},"session":{"id":"x"}}`))
	}))
	defer srv.Close()
	token := r87btoolsJWT("team_claims", "prj_claims", time.Now().Add(time.Hour))
	c := NewAPIClient(srv.URL, Credentials{Token: token, TeamID: "team_x", ProjectID: "prj_x"})
	if _, err := c.CreateSandbox(t.Context(), createSandboxRequest{}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "teamId=team_claims" || gotBody["projectId"] != "prj_x" {
		t.Fatalf("query %q body projectId %v", gotQuery, gotBody["projectId"])
	}
}

// decodeUnverifiedToken (api-client.js 72-83): Buffer.from(…, "base64url")
// skips other characters and stops at "=", owner_id is tested for JS
// truthiness and the team is its String() in the query. Expected values from
// node running the 3.9.0 function on the same tokens.
func TestR87bintegUnverifiedTokenClaimsJSSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, token, team, project string
		ok                         bool
	}{
		{"number", "h.eyJvd25lcl9pZCI6MTIzLCJwcm9qZWN0X2lkIjo3fQ.s", "123", "7", true},
		{"empty", "h.eyJvd25lcl9pZCI6IiIsInByb2plY3RfaWQiOiJwIn0.s", "", "", false},
		{"no project", "h.eyJvd25lcl9pZCI6Im8ifQ.s", "o", "", true},
		{"null project", "h.eyJvd25lcl9pZCI6Im8iLCJwcm9qZWN0X2lkIjpudWxsfQ.s", "o", "", true},
		{"junk", "h.eyJv!*d25lcl9pZCI6InRlYW1faiIsInByb2plY3RfaWQiOiJwcmpfaiJ9.s", "team_j", "prj_j", true},
		{"padded", "h.eyJvd25lcl9pZCI6InRlYW1fcCIsInByb2plY3RfaWQiOiJwcmpfcCJ9==.s", "team_p", "prj_p", true},
		{"null payload", "h.bnVsbA.s", "", "", false},
		{"false owner", "h.eyJvd25lcl9pZCI6ZmFsc2V9.s", "", "", false},
		{"two parts", "h.eyJvd25lcl9pZCI6Im8ifQ", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			team, project, ok := unverifiedTokenClaims(tc.token)
			if team != tc.team || project != tc.project || ok != tc.ok {
				t.Fatalf("got %q %q %v, want %q %q %v", team, project, ok, tc.team, tc.project, tc.ok)
			}
		})
	}
}

// V061 41 (known difference): Go gives the TS fast-consumer result. Node run
// of the 3.9.0 readJsonLines / pipe code with jsonlines 0.1.1, the body
// arriving as two network chunks 20 ms apart (40 records, then the last
// line), a consumer that keeps up: `bad\n` is a SyntaxError after 40
// records; an unterminated `bad` is a ConnectionError ("Stream ended with an
// incomplete JSON record") after 40 records.
func TestR87bintegJSONLinesFastConsumerResult(t *testing.T) {
	var recs strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&recs, "{\"i\":%d}\n", i)
	}
	for _, tc := range []struct {
		last string
		conn bool
	}{{"bad\n", false}, {"bad", true}} {
		n := 0
		err := readJSONLines(strings.NewReader(recs.String()+tc.last), "s", func([]byte) error { n++; return nil })
		var connErr *connectionError
		if n != 40 || err == nil || errors.As(err, &connErr) != tc.conn {
			t.Fatalf("%q: %d records, err %v (connection error %v)", tc.last, n, err, errors.As(err, &connErr))
		}
		if tc.conn && connErr.message != "Stream ended with an incomplete JSON record" {
			t.Fatalf("message = %q", connErr.message)
		}
	}
}

// tools c0d8b859 survivors: 3.9.0 session.js extendTimeout and update
// replace `this.session` with the response session, so the next
// Sandbox.update computes its increment from the timeout the API returned
// (here 90000 after the extend, 100000 after the policy update).
func TestR87bintegUpdateTracksSessionTimeoutFromResponses(t *testing.T) {
	client, reqs, mu := r87toolsWireServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = fmt.Fprintf(w, r87btoolsSandboxJSON, "running", 60000)
		case strings.HasSuffix(r.URL.Path, "/extend-timeout"):
			_, _ = io.WriteString(w, `{"session":{"id":"sess","status":"running","timeout":90000}}`)
		case strings.HasSuffix(r.URL.Path, "/network-policy"):
			_, _ = io.WriteString(w, `{"session":{"id":"sess","status":"running","timeout":100000,"networkPolicy":{"mode":"deny-all"}}}`)
		default:
			_, _ = io.WriteString(w, `{"sandbox":{"name":"sb","persistent":true}}`)
		}
	})
	sb, err := GetSandbox(context.Background(), client, "sb", nil)
	if err != nil {
		t.Fatal(err)
	}
	deny := DenyAllNetworkPolicy()
	for _, p := range []UpdateParams{
		{TimeoutMs: r87btoolsInt64(90000)},                       // extend 30000 -> session 90000
		{TimeoutMs: r87btoolsInt64(90000), NetworkPolicy: &deny}, // no extend; policy -> session 100000
		{TimeoutMs: r87btoolsInt64(120000)},                      // extend 20000
	} {
		if err := sb.Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	var extends []string
	for _, r := range *reqs {
		if strings.HasSuffix(r.path, "/extend-timeout") {
			extends = append(extends, strings.TrimSpace(r.body))
		}
	}
	if strings.Join(extends, " ") != `{"duration":30000} {"duration":20000}` {
		t.Fatalf("extend-timeout bodies = %v", extends)
	}
}

// tools affeb762 survivors (3.9.0 get-credentials.js): explicit credentials,
// even partial ones, never reach getVercelToken (getCredentialsFromParams
// throws "Missing credentials parameters ..." first); with VERCEL_URL set
// the failure is the VercelOidcContextError text; a cancelled context is
// reported as such (Go: no TS twin for cancellation).
func TestR87bintegOIDCRefreshCredentialPaths(t *testing.T) {
	r87btoolsOIDCEnv(t, "", "", false)
	_, err := resolveCredentialsWithRefresh(context.Background(), Credentials{TeamID: "team_x"}, &OIDCRefresh{})
	if !errors.Is(err, ErrPartialCredentials) {
		t.Fatalf("partial explicit credentials: err = %v", err)
	}

	t.Setenv("VERCEL_URL", "app.vercel.app")
	_, err = resolveCredentialsWithRefresh(context.Background(), Credentials{}, &OIDCRefresh{})
	var ctxErr *vercelOIDCContextError
	want := "Could not get credentials from OIDC context.\nPlease make sure OIDC is set up for your project\n╰▶ Docs: https://vercel.com/docs/oidc"
	if !errors.As(err, &ctxErr) || ctxErr.Error() != want {
		t.Fatalf("VERCEL_URL set: err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolveCredentialsWithRefresh(ctx, Credentials{}, &OIDCRefresh{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
}

package vercel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		{"array owner", "h.eyJvd25lcl9pZCI6WyJhIiwxXSwicHJvamVjdF9pZCI6eyJ4IjoxfX0.s", "a,1", "[object Object]", true},
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

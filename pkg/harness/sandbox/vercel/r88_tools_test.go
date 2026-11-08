package vercel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Credential transports class sweep (r88 Go-only item, S2): the OIDC
// refresh transport adds the token only to requests for the API origin.
// A redirect to another origin gets no Authorization header and no team
// query (fetch drops Authorization on a cross-origin redirect); a
// same-origin redirect keeps the token and follows its Location as given.
func TestR88toolsOIDCTransportCredentialsStayOnTheAPIOrigin(t *testing.T) {
	r87ctoolsEnv(t)
	token := r87ctoolsRawJWT(fmt.Sprintf(`{"owner_id":"team_1","project_id":"prj_1","exp":%d}`, time.Now().Add(time.Hour).Unix()))
	t.Setenv("VERCEL_OIDC_TOKEN", token)

	var mu sync.Mutex
	var foreignAuth, foreignQuery string
	foreignHit := false
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		foreignHit, foreignAuth, foreignQuery = true, r.Header.Get("Authorization"), r.URL.RawQuery
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer foreign.Close()
	var sameAuth, sameQuery string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/sandboxes/away":
			http.Redirect(w, r, foreign.URL+"/elsewhere", http.StatusFound)
		case "/v2/sandboxes/moved":
			http.Redirect(w, r, "/v2/sandboxes/target?x=1", http.StatusFound)
		case "/v2/sandboxes/target":
			mu.Lock()
			sameAuth, sameQuery = r.Header.Get("Authorization"), r.URL.RawQuery
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer api.Close()

	c := newAPIClient(api.URL, Credentials{Token: token, TeamID: "team_1", ProjectID: "prj_1"}, &OIDCRefresh{})
	resume := false
	_, _ = c.GetSandbox(context.Background(), "away", &resume)
	_, _ = c.GetSandbox(context.Background(), "moved", &resume)
	mu.Lock()
	defer mu.Unlock()
	if !foreignHit || foreignAuth != "" || foreignQuery != "" {
		t.Fatalf("foreign origin got hit=%v Authorization %q query %q", foreignHit, foreignAuth, foreignQuery)
	}
	if sameAuth != "Bearer "+token || sameQuery != "x=1" {
		t.Fatalf("same-origin redirect got Authorization %q query %q", sameAuth, sameQuery)
	}
}

// The plain-token client follows the same rule, and a chain that leaves the
// API origin and comes back stays without the header (fetch removes it for
// the rest of the chain; net/http would copy it again).
func TestR88toolsPlainClientDropsCredentialsAcrossOrigins(t *testing.T) {
	var mu sync.Mutex
	auths := map[string]string{}
	var api *httptest.Server
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths["foreign"] = r.Header.Get("Authorization")
		mu.Unlock()
		http.Redirect(w, r, api.URL+"/back", http.StatusFound)
	}))
	defer foreign.Close()
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/back" {
			mu.Lock()
			auths["back"] = r.Header.Get("Authorization")
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		http.Redirect(w, r, foreign.URL+"/x", http.StatusFound)
	}))
	defer api.Close()
	resume := false
	_, _ = NewAPIClient(api.URL, Credentials{Token: "plain-token", TeamID: "team_1"}).GetSandbox(context.Background(), "sb", &resume)
	mu.Lock()
	defer mu.Unlock()
	if v, ok := auths["foreign"]; !ok || v != "" {
		t.Fatalf("foreign Authorization = %q (hit %v)", v, ok)
	}
	if v, ok := auths["back"]; !ok || v != "" {
		t.Fatalf("back on the API origin Authorization = %q (hit %v)", v, ok)
	}
}

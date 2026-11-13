package vercel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Round 87e: known differences fixed against @vercel/sandbox 3.9.0
// (utils/paginator.js attachPaginator, sandbox.js listSessions,
// api-client/base-client.js `array(value).forEach(... append ...)`) and TS
// sandbox-vercel vercel-network-policy-manager.ts #inspectPolicy.

type r87etoolsRecorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *r87etoolsRecorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req.Method+" "+req.URL.RequestURI())
}

func (r *r87etoolsRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
}

// attachPaginator follows `pagination.next` until it is null, with no page
// bound (v0.6.0 before this fix stopped after 20 pages).
func TestR87etoolsSnapshotListingHasNoPageBound(t *testing.T) {
	const pages = 25
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 0
		if c := r.URL.Query().Get("cursor"); c != "" {
			_, _ = fmt.Sscanf(c, "p%d", &page)
		}
		next := "null"
		if page+1 < pages {
			next = fmt.Sprintf(`"p%d"`, page+1)
		}
		_, _ = fmt.Fprintf(w, `{"snapshots":[{"id":"s%d","status":"created","creationMethod":"manual"}],"pagination":{"count":1,"next":%s}}`, page, next)
	}))
	defer srv.Close()
	c := NewAPIClient(srv.URL, Credentials{Token: "plain", TeamID: "team", ProjectID: "prj"})
	got, err := c.ListSnapshots(context.Background(), "tpl")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != pages || got[pages-1].ID != fmt.Sprintf("s%d", pages-1) {
		t.Fatalf("got %d snapshots, want %d", len(got), pages)
	}
}

// r87etoolsSessionsServer serves one sandbox session listing and records
// every request; a POST (a policy mutation) answers with the session.
func r87etoolsSessionsServer(t *testing.T, rec *r87etoolsRecorder, list func(cursor string) (int, string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/sandboxes/sessions":
			status, body := list(r.URL.Query().Get("cursor"))
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/network-policy"):
			_, _ = w.Write([]byte(`{"session":{"id":"session","cwd":"/vercel/sandbox","status":"running","timeout":120000,"networkPolicy":{"mode":"custom","allowedDomains":["example.com"]}}}`))
		case r.Method == http.MethodPatch || r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"sandbox":{"name":"sbx","persistent":false}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func r87etoolsObjectPolicySandbox(t *testing.T, url string) *Sandbox {
	t.Helper()
	body := `{"sandbox":{"name":"sbx","persistent":false,"currentSessionId":"session"},` +
		`"session":{"id":"session","cwd":"/vercel/sandbox","status":"running","timeout":120000,"networkPolicy":{"mode":"custom","allowedDomains":["example.com"]}},"routes":[]}`
	var resp sandboxAndSessionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	return newSandboxFromResponse(NewAPIClient(url, Credentials{Token: "t", TeamID: "team", ProjectID: "p"}), &resp)
}

// "rejects when the current session policy cannot be inspected": the
// session is not listed, listed under another id, or listed without a
// networkPolicy. The listing comes from the API on every mutation, not from
// the last session response.
func TestR87etoolsPolicyInspectionListsSessions(t *testing.T) {
	for _, sessions := range []string{
		`[]`,
		`[{"id":"other-session","networkPolicy":{"mode":"custom"}}]`,
		`[{"id":"session"}]`,
	} {
		rec := &r87etoolsRecorder{}
		srv := r87etoolsSessionsServer(t, rec, func(string) (int, string) {
			return 200, `{"sessions":` + sessions + `,"pagination":{"count":1,"next":null}}`
		})
		sbx := r87etoolsObjectPolicySandbox(t, srv.URL)
		err := NewPolicyManager(sbx).AddRequestTransformations(context.Background(), r85toolsTransformations)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "could not be inspected") {
			t.Fatalf("%s: err = %v", sessions, err)
		}
		if got := strings.Join(rec.all(), "\n"); got != "GET /v2/sandboxes/sessions?teamId=team&project=p&name=sbx" {
			t.Fatalf("%s: requests:\n%s", sessions, got)
		}
	}

	// A failed listing rejects the mutation with its error; the next one
	// lists again and proceeds.
	rec := &r87etoolsRecorder{}
	calls := 0
	srv := r87etoolsSessionsServer(t, rec, func(string) (int, string) {
		calls++
		if calls == 1 {
			return 400, `{"error":{"message":"inspection failed"}}`
		}
		return 200, `{"sessions":[{"id":"session","networkPolicy":{"mode":"custom","allowedDomains":["example.com"],"responseRules":[]}}],"pagination":{"count":1,"next":null}}`
	})
	defer srv.Close()
	sbx := r87etoolsObjectPolicySandbox(t, srv.URL)
	m := NewPolicyManager(sbx)
	err := m.AddRequestTransformations(context.Background(), r85toolsTransformations)
	if err == nil || !strings.Contains(err.Error(), "inspection failed") {
		t.Fatalf("first mutation: err = %v", err)
	}
	if err := m.AddRequestTransformations(context.Background(), r85toolsTransformations); err != nil {
		t.Fatalf("second mutation: %v", err)
	}
	reqs := rec.all()
	if len(reqs) < 3 || !strings.HasPrefix(reqs[1], "GET /v2/sandboxes/sessions?") || strings.HasPrefix(reqs[2], "GET ") {
		t.Fatalf("requests:\n%s", strings.Join(reqs, "\n"))
	}
}

// base-client.js appends one `teamId` per element of an array team (the
// owner_id claim), none for an empty array; a null element throws the
// TypeError of `null.toString()` before the request is sent.
func TestR87etoolsOwnerIDArrayTeamQuery(t *testing.T) {
	for _, tc := range []struct {
		owner, want, err string
	}{
		{`["team_a","team_b"]`, "GET /v2/sandboxes/sbx?teamId=team_a&teamId=team_b&projectId=prj", ""},
		{`[]`, "GET /v2/sandboxes/sbx?projectId=prj", ""},
		{`[1,["x","y"]]`, "GET /v2/sandboxes/sbx?teamId=1&teamId=x%2Cy&projectId=prj", ""},
		{`"team_s"`, "GET /v2/sandboxes/sbx?teamId=team_s&projectId=prj", ""},
		{`["team_a",null]`, "", "Cannot read properties of null (reading 'toString')"},
	} {
		rec := &r87etoolsRecorder{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			_, _ = w.Write([]byte(`{"sandbox":{"name":"sbx"},"session":{"id":"s"},"routes":[]}`))
		}))
		token := r87ctoolsRawJWT(`{"owner_id":` + tc.owner + `,"project_id":"prj"}`)
		c := NewAPIClient(srv.URL, Credentials{Token: token, TeamID: "explicit", ProjectID: "prj"})
		_, err := c.GetSandbox(context.Background(), "sbx", nil)
		srv.Close()
		if tc.err != "" {
			if err == nil || err.Error() != tc.err || len(rec.all()) != 0 {
				t.Fatalf("%s: err = %v, requests %v", tc.owner, err, rec.all())
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.owner, err)
		}
		if got := strings.Join(rec.all(), "\n"); got != tc.want {
			t.Fatalf("%s: request %q, want %q", tc.owner, got, tc.want)
		}
	}

	// A refreshed array team replaces the previous team at the front.
	team := teamQueryOf([]any{"n1", "n2"})
	if got := team.withTeam("teamId=old&project=p&name=x"); got != "teamId=n1&teamId=n2&project=p&name=x" {
		t.Fatalf("withTeam = %q", got)
	}
	if got := teamQueryOf([]any{}).withTeam("teamId=old&a=1"); got != "a=1" {
		t.Fatalf("withTeam(empty) = %q", got)
	}
}

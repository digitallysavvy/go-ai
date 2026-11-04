package vercel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// TS utils.ts isSandboxNameConflictError over the @vercel/sandbox 3.9.0
// APIError: message "Status code N is not ok: <server message>" and
// json.error.message; the Go error prefix "vercel sandbox API" is not tested.
func TestR87toolsSandboxNameConflictUsesTSMessages(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{409, `{"error":{"message":"Sandbox name already exists"}}`, true},
		{400, `{"error":{"message":"a sandbox with this name already exists"}}`, true},
		{409, `{"error":{"message":"name already exists"}}`, false},
		{409, `{"error":{"message":"Sandbox name already exists","code":7}}`, true},
		{500, `{"error":{"message":"Sandbox name already exists"}}`, false},
		{409, `{}`, false},
	} {
		err := toAPIError(&httpinternal.HTTPStatusError{StatusCode: tc.status, Body: []byte(tc.body)})
		if got := isSandboxNameConflictError(err); got != tc.want {
			t.Errorf("%d %s: conflict = %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}

type r87toolsRecorded struct {
	method, path, body string
}

// r87toolsWireServer records every request and answers from respond.
func r87toolsWireServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, body string)) (*APIClient, *[]r87toolsRecorded, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var reqs []r87toolsRecorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, r87toolsRecorded{r.Method, r.URL.Path, string(b)})
		mu.Unlock()
		respond(w, r, string(b))
	}))
	t.Cleanup(srv.Close)
	return NewAPIClient(srv.URL, Credentials{Token: "t", ProjectID: "p"}), &reqs, &mu
}

const r87toolsSandboxJSON = `{"sandbox":{"name":"sb","persistent":true},"session":{"id":"sess","status":"%s","cwd":"/vercel/sandbox"},"routes":[]}`

// @vercel/sandbox 3.9.0 toAPINetworkPolicy: "deny-all" / "allow-all" go out
// as {"mode": ...} on create, PATCH and the session policy update; objects
// keep their public shape.
func TestR87toolsNetworkPolicyRequestShape(t *testing.T) {
	client, reqs, mu := r87toolsWireServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v3/sandboxes":
			fmt.Fprintf(w, r87toolsSandboxJSON, "running")
		case strings.HasSuffix(r.URL.Path, "/network-policy"):
			_, _ = io.WriteString(w, `{"session":{"id":"sess","status":"running","networkPolicy":{"mode":"deny-all"}}}`)
		default:
			_, _ = io.WriteString(w, `{"sandbox":{"name":"sb","persistent":true}}`)
		}
	})
	deny := DenyAllNetworkPolicy()
	sb, err := CreateSandbox(context.Background(), client, CreateParams{Name: "sb", NetworkPolicy: &deny})
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.UpdateNetworkPolicy(context.Background(), AllowAllNetworkPolicy()); err != nil {
		t.Fatal(err)
	}
	if err := sb.UpdateNetworkPolicy(context.Background(), NetworkPolicy{AllowList: []string{"a.example"}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []struct{ method, path, policy string }{
		{"POST", "/v3/sandboxes", `{"mode":"deny-all"}`},
		{"POST", "/v2/sandboxes/sessions/sess/network-policy", `{"mode":"allow-all"}`},
		{"PATCH", "/v2/sandboxes/sb", `{"mode":"allow-all"}`},
		{"POST", "/v2/sandboxes/sessions/sess/network-policy", `{"allow":["a.example"]}`},
		{"PATCH", "/v2/sandboxes/sb", `{"allow":["a.example"]}`},
	}
	if len(*reqs) != len(want) {
		t.Fatalf("requests = %+v", *reqs)
	}
	for i, w := range want {
		got := (*reqs)[i]
		policy := got.body
		if !strings.HasSuffix(got.path, "/network-policy") {
			var body struct {
				NetworkPolicy json.RawMessage `json:"networkPolicy"`
			}
			_ = json.Unmarshal([]byte(got.body), &body)
			policy = string(body.NetworkPolicy)
		}
		if got.method != w.method || got.path != w.path || policy != w.policy {
			t.Errorf("request %d = %s %s %s, want %s %s %s", i, got.method, got.path, policy, w.method, w.path, w.policy)
		}
	}
}

// 3.9.0 Sandbox.update skips the session policy update for a stopped,
// stopping, aborted or failed session and still PATCHes the sandbox.
func TestR87toolsUpdateSkipsInactiveSession(t *testing.T) {
	for _, status := range []string{"stopped", "stopping", "aborted", "failed"} {
		client, reqs, mu := r87toolsWireServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
			if r.URL.Path == "/v2/sandboxes/sb" && r.Method == http.MethodGet {
				fmt.Fprintf(w, r87toolsSandboxJSON, status)
				return
			}
			_, _ = io.WriteString(w, `{"sandbox":{"name":"sb","persistent":true}}`)
		})
		sb, err := GetSandbox(context.Background(), client, "sb", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := sb.UpdateNetworkPolicy(context.Background(), DenyAllNetworkPolicy()); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if len(*reqs) != 2 || (*reqs)[1].method != http.MethodPatch {
			t.Errorf("%s: requests = %+v", status, *reqs)
		}
		mu.Unlock()
	}
}

// A policy the session rejects is not persisted (session update first).
func TestR87toolsUpdateSessionFailureSkipsPatch(t *testing.T) {
	client, reqs, mu := r87toolsWireServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case r.Method == http.MethodGet:
			fmt.Fprintf(w, r87toolsSandboxJSON, "running")
		case strings.HasSuffix(r.URL.Path, "/network-policy"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"bad policy"}}`)
		default:
			_, _ = io.WriteString(w, `{"sandbox":{"name":"sb"}}`)
		}
	})
	sb, err := GetSandbox(context.Background(), client, "sb", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.UpdateNetworkPolicy(context.Background(), DenyAllNetworkPolicy()); err == nil {
		t.Fatal("expected the session error")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, r := range *reqs {
		if r.method == http.MethodPatch {
			t.Fatalf("PATCH sent after the session rejected the policy: %+v", *reqs)
		}
	}
}

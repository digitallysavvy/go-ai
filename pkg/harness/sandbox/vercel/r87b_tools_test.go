package vercel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
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

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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
			_, _ = fmt.Fprintf(w, r87toolsSandboxJSON, "running")
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
				_, _ = fmt.Fprintf(w, r87toolsSandboxJSON, status)
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
			_, _ = fmt.Fprintf(w, r87toolsSandboxJSON, "running")
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

// r87toolsStdinServer serves a detached command and a stdin endpoint
// answered by stdin (status, body) per request.
func r87toolsStdinServer(t *testing.T, stdin func(n int, body string) (int, string)) (*Sandbox, *[]r87toolsRecorded, *sync.Mutex) {
	t.Helper()
	n := 0
	client, reqs, mu := r87toolsWireServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/sandboxes/sb":
			_, _ = fmt.Fprintf(w, r87toolsSandboxJSON, "running")
		case strings.HasSuffix(r.URL.Path, "/stdin"):
			n++
			status, out := stdin(n, body)
			if status == 429 {
				w.Header().Set("Retry-After", "0")
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, out)
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.Header().Set("Content-Type", "application/x-ndjson")
		case strings.HasSuffix(r.URL.Path, "/cmd"):
			_, _ = io.WriteString(w, `{"command":{"id":"c1","name":"bash","args":[],"cwd":"/","sessionId":"sess","exitCode":null,"startedAt":1}}`)
		default:
			_, _ = io.WriteString(w, `{"command":{"id":"c1","exitCode":0}}`)
		}
	})
	sb, err := GetSandbox(context.Background(), client, "sb", nil)
	if err != nil {
		t.Fatal(err)
	}
	return sb, reqs, mu
}

func r87toolsStdinBodies(reqs []r87toolsRecorded) []string {
	var out []string
	for _, r := range reqs {
		if strings.HasSuffix(r.path, "/stdin") {
			out = append(out, r.body)
		}
	}
	return out
}

func r87toolsFastStdinBackoff(t *testing.T) {
	old := stdinBackoff
	stdinBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { stdinBackoff = old })
}

// @vercel/sandbox 3.9.0: spawn with stdin sends attachStdin; the first
// write probes the offset ({}), data goes out base64 with the offset, and
// closeStdin sends {offset, close: true}.
func TestR87toolsSpawnStdinWireShape(t *testing.T) {
	written := 0
	sb, reqs, mu := r87toolsStdinServer(t, func(n int, body string) (int, string) {
		var req struct {
			Data string `json:"data"`
		}
		_ = json.Unmarshal([]byte(body), &req)
		b, _ := base64.StdEncoding.DecodeString(req.Data)
		written += len(b)
		return 200, fmt.Sprintf(`{"command":{"id":"c1"},"bytesWritten":%d}`, written)
	})
	p, err := NewSession(sb).Spawn(context.Background(), providerutils.SandboxProcessOptions{Command: "cat", Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	wp, ok := p.(providerutils.WritableSandboxProcess)
	if !ok {
		t.Fatalf("Spawn with Stdin returned %T, want a WritableSandboxProcess", p)
	}
	if n, err := wp.Stdin().Write([]byte("hello")); n != 5 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if n, err := wp.Stdin().Write(nil); n != 0 || err != nil {
		t.Fatalf("empty Write = %d, %v", n, err)
	}
	if err := wp.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := wp.Stdin().Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write after Close = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	var cmdBody string
	for _, r := range *reqs {
		if strings.HasSuffix(r.path, "/cmd") {
			cmdBody = r.body
		}
	}
	if !strings.Contains(cmdBody, `"attachStdin":true`) {
		t.Fatalf("cmd body = %s", cmdBody)
	}
	got := r87toolsStdinBodies(*reqs)
	want := []string{`{}`, `{"data":"aGVsbG8=","offset":0}`, `{"offset":5,"close":true}`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stdin bodies = %q, want %q", got, want)
	}
}

// Writes over 512 KiB go out in STDIN_CHUNK_BYTES chunks at increasing
// offsets (offset + chunk length when the API reports no bytesWritten).
func TestR87toolsSpawnStdinChunks(t *testing.T) {
	sb, reqs, mu := r87toolsStdinServer(t, func(n int, _ string) (int, string) {
		if n == 1 {
			return 200, `{"command":{"id":"c1"},"bytesWritten":10}`
		}
		return 200, `{"command":{"id":"c1"}}`
	})
	proc, err := sb.SpawnCommandWithStdin(context.Background(), "cat", nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, stdinChunkBytes+3)
	if n, err := proc.Stdin().Write(data); n != len(data) || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	var offsets []int64
	for _, b := range r87toolsStdinBodies(*reqs)[1:] {
		var req struct {
			Data   string `json:"data"`
			Offset int64  `json:"offset"`
		}
		_ = json.Unmarshal([]byte(b), &req)
		raw, _ := base64.StdEncoding.DecodeString(req.Data)
		offsets = append(offsets, req.Offset, int64(len(raw)))
	}
	if want := []int64{10, stdinChunkBytes, 10 + stdinChunkBytes, 3}; !reflect.DeepEqual(offsets, want) {
		t.Fatalf("offset/len pairs = %v, want %v", offsets, want)
	}
}

// A resendable write (with an offset) is retried on 503 and 429; a failed
// write errors the stream so Close neither succeeds nor sends EOF.
func TestR87toolsSpawnStdinRetriesAndErrorsStream(t *testing.T) {
	r87toolsFastStdinBackoff(t)
	sb, reqs, mu := r87toolsStdinServer(t, func(n int, _ string) (int, string) {
		switch n {
		case 1:
			return 200, `{"command":{"id":"c1"},"bytesWritten":0}`
		case 2:
			return 503, `{}`
		case 3:
			return 429, `{}`
		case 4:
			return 200, `{"command":{"id":"c1"},"bytesWritten":2}`
		default:
			return 400, `{"error":{"message":"bad","code":"command_stdin_unavailable"}}`
		}
	})
	proc, err := sb.SpawnCommandWithStdin(context.Background(), "cat", nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proc.Stdin().Write([]byte("ab")); err != nil {
		t.Fatalf("Write after retries: %v", err)
	}
	_, werr := proc.Stdin().Write([]byte("cd"))
	var apiErr *APIError
	if !errors.As(werr, &apiErr) || apiErr.StatusCode != 400 || apiErr.Code != "command_stdin_unavailable" {
		t.Fatalf("Write err = %v", werr)
	}
	if err := proc.Stdin().Close(); err != werr {
		t.Fatalf("Close after a failed write = %v, want the write error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := len(r87toolsStdinBodies(*reqs)); got != 5 {
		t.Fatalf("stdin requests = %d, want 5 (no close after the failure)", got)
	}
}

// TS writeCommandStdin retry rules on the client call itself.
func TestR87toolsWriteCommandStdinRetryRules(t *testing.T) {
	r87toolsFastStdinBackoff(t)
	zero := int64(0)
	cases := []struct {
		name      string
		params    writeStdinParams
		responses []string // "status body"
		wantReqs  int
		wantErr   bool
		wantBytes *int64
	}{
		{"no offset: 503 is not retried", writeStdinParams{data: []byte("x")}, []string{`503 {}`}, 1, true, nil},
		{"no offset: 429 is retried", writeStdinParams{data: []byte("x")}, []string{`429 {}`, `200 {"bytesWritten":1}`}, 2, false, r87toolsI64(1)},
		{"504 with progress continues, stalled twice fails", writeStdinParams{data: []byte("x"), offset: &zero},
			[]string{`504 {"error":{"bytesWritten":1}}`, `504 {"error":{"bytesWritten":1}}`, `504 {}`}, 3, true, nil},
		{"504 then success", writeStdinParams{data: []byte("x"), offset: &zero}, []string{`504 {}`, `200 {"bytesWritten":1}`}, 2, false, r87toolsI64(1)},
		{"close 404 after a maybe-applied attempt succeeds", writeStdinParams{offset: &zero, close: true}, []string{`502 {}`, `404 {}`}, 2, false, nil},
		{"close 404 on the first attempt fails", writeStdinParams{offset: &zero, close: true}, []string{`404 {}`}, 1, true, nil},
		{"retries stop after five", writeStdinParams{offset: &zero}, []string{`503 {}`, `503 {}`, `503 {}`, `503 {}`, `503 {}`, `503 {}`}, 6, true, nil},
		{"negative bytesWritten is invalid", writeStdinParams{}, []string{`200 {"bytesWritten":-1}`}, 1, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			n := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				resp := tc.responses[n]
				n++
				mu.Unlock()
				status, body, _ := strings.Cut(resp, " ")
				code := 0
				_, _ = fmt.Sscanf(status, "%d", &code)
				w.WriteHeader(code)
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()
			c := NewAPIClient(srv.URL, Credentials{Token: "t"})
			got, err := c.writeCommandStdin(context.Background(), "s", "c", tc.params)
			if (err != nil) != tc.wantErr || n != tc.wantReqs || !reflect.DeepEqual(got, tc.wantBytes) {
				t.Fatalf("got %v, %v after %d requests; want err=%v after %d, bytes %v", got, err, n, tc.wantErr, tc.wantReqs, tc.wantBytes)
			}
		})
	}
}

func r87toolsI64(v int64) *int64 { return &v }

// The offset probe must report bytesWritten (TS "The API did not report the
// bytes written to stdin.").
func TestR87toolsSpawnStdinOffsetProbeNeedsBytesWritten(t *testing.T) {
	sb, _, _ := r87toolsStdinServer(t, func(int, string) (int, string) { return 200, `{"command":{"id":"c1"}}` })
	proc, err := sb.SpawnCommandWithStdin(context.Background(), "cat", nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proc.Stdin().Write([]byte("x")); err == nil || err.Error() != "The API did not report the bytes written to stdin." {
		t.Fatalf("Write = %v", err)
	}
}

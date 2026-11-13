package vercel

import (
	"context"
	"errors"
	"strings"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fetch gives a 204 / 205 reply a null body: 3.9.0 runCommand and
// getLogsStream throw APIError "No response body", and readFile returns
// null. net/http hands back http.NoBody, which Go read as an empty stream
// (stream_ended_early) or an empty file.
func TestR99toolsNullBodyStatuses(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/sandboxes/sessions/s/fs/read" {
				w.Header().Set("Content-Type", "application/octet-stream")
			} else {
				w.Header().Set("Content-Type", "application/x-ndjson")
			}
			w.WriteHeader(status)
		}))
		client := NewAPIClient(srv.URL, Credentials{Token: "tok"})
		_, err := client.RunCommandWait(t.Context(), "s", runCommandRequest{Command: "echo"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Message != "No response body" || apiErr.StatusCode != status {
			t.Errorf("%d: RunCommandWait err = %v", status, err)
		}
		err = client.GetLogs(t.Context(), "s", "c", func(LogEntry) {})
		if !errors.As(err, &apiErr) || apiErr.Message != "No response body" {
			t.Errorf("%d: GetLogs err = %v", status, err)
		}
		rc, err := client.ReadFile(t.Context(), "s", "/f", "")
		if rc != nil || err != nil {
			t.Errorf("%d: ReadFile = %v, %v; want nil, nil", status, rc, err)
		}
		srv.Close()
	}
}

// r99toolsSchemaServer answers every request with status 200 and body.
func r99toolsSchemaServer(t *testing.T, body string) *APIClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewAPIClient(srv.URL, Credentials{Token: "tok"})
}

func r99toolsWantInvalid(t *testing.T, name string, err error, wantIssues string) {
	t.Helper()
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.HasPrefix(apiErr.Message, "Response JSON is not valid: [") {
		t.Errorf("%s: err = %v, want the invalid-response APIError", name, err)
		return
	}
	if wantIssues != "" && apiErr.Message != "Response JSON is not valid: "+wantIssues {
		t.Errorf("%s: message = %q\nwant %q", name, apiErr.Message, "Response JSON is not valid: "+wantIssues)
	}
}

// 3.9.0 base-client.js parseOrThrow validates every API response against
// its validators.js schema; a mismatch is the APIError "Response JSON is not
// valid: <ZodError>". Go decoded leniently (missing fields read as zero
// values). One case per schema family: a complete response passes, a
// malformed one is rejected. The two exact texts were produced with node
// and zod 4 (the zod major @vercel/sandbox 3.9.0 depends on).
func TestR99toolsResponseSchemas(t *testing.T) {
	ctx := context.Background()
	session := `{"id":"s1","memory":2048,"vcpus":1,"region":"iad1","timeout":300000,"status":"running","requestedAt":1,"createdAt":1,"cwd":"/vercel/sandbox","updatedAt":1}`
	sandbox := `{"name":"sb","persistent":false,"createdAt":1,"updatedAt":1,"currentSessionId":"s1","status":"running"}`
	snapshot := `{"id":"snap","sourceSessionId":"s1","region":"iad1","status":"created","sizeBytes":0,"createdAt":1,"updatedAt":1}`
	command := `{"id":"c","name":"sh","args":[],"cwd":"/","sessionId":"s1","exitCode":null,"startedAt":1}`
	finished := `{"id":"c","name":"sh","args":[],"cwd":"/","sessionId":"s1","exitCode":0,"startedAt":1}`

	// SandboxAndSessionResponse (create / get sandbox).
	if _, err := r99toolsSchemaServer(t, `{"sandbox":`+sandbox+`,"session":`+session+`,"routes":[]}`).GetSandbox(ctx, "sb", nil); err != nil {
		t.Errorf("valid GetSandbox: %v", err)
	}
	_, err := r99toolsSchemaServer(t, `{"sandbox":{"name":"sb"},"session":`+session+`,"routes":[]}`).GetSandbox(ctx, "sb", nil)
	r99toolsWantInvalid(t, "GetSandbox sandbox without persistent", err, "")
	// Session.networkPolicy is NetworkPolicyResponseValidator: the request
	// (V2) shape is not a response shape.
	_, err = r99toolsSchemaServer(t, `{"sandbox":`+sandbox+`,"session":{"id":"s1","memory":2048,"vcpus":1,"region":"iad1","timeout":300000,"status":"running","requestedAt":1,"createdAt":1,"cwd":"/","updatedAt":1,"networkPolicy":{"allow":["a"]}},"routes":[]}`).GetSandbox(ctx, "sb", nil)
	r99toolsWantInvalid(t, "GetSandbox V2 policy", err, "")
	// UpdateSandboxResponse.
	if _, err := r99toolsSchemaServer(t, `{"sandbox":`+sandbox+`}`).UpdateSandbox(ctx, "sb", updateSandboxRequest{}); err != nil {
		t.Errorf("valid UpdateSandbox: %v", err)
	}
	_, err = r99toolsSchemaServer(t, `{"sandbox":{"name":"sb","persistent":"yes"}}`).UpdateSandbox(ctx, "sb", updateSandboxRequest{})
	r99toolsWantInvalid(t, "UpdateSandbox", err, "")
	// StopSessionResponse.
	if _, err := r99toolsSchemaServer(t, `{"session":`+session+`,"snapshot":`+snapshot+`}`).StopSession(ctx, "s1"); err != nil {
		t.Errorf("valid StopSession: %v", err)
	}
	_, err = r99toolsSchemaServer(t, `{"session":`+session+`,"snapshot":{"id":"snap"}}`).StopSession(ctx, "s1")
	r99toolsWantInvalid(t, "StopSession snapshot", err, "")
	// CreateSnapshotResponse and SnapshotsResponse.
	if _, err := r99toolsSchemaServer(t, `{"snapshot":`+snapshot+`,"session":`+session+`}`).CreateSnapshot(ctx, "s1", nil); err != nil {
		t.Errorf("valid CreateSnapshot: %v", err)
	}
	_, err = r99toolsSchemaServer(t, `{"snapshot":{"id":"snap","sourceSessionId":"s1","region":"iad1","status":"pending","sizeBytes":0,"createdAt":1,"updatedAt":1},"session":`+session+`}`).CreateSnapshot(ctx, "s1", nil)
	r99toolsWantInvalid(t, "CreateSnapshot status", err, "")
	if _, err := r99toolsSchemaServer(t, `{"snapshots":[`+snapshot+`],"pagination":{"count":1,"next":null}}`).ListSnapshots(ctx, "sb"); err != nil {
		t.Errorf("valid ListSnapshots: %v", err)
	}
	_, err = r99toolsSchemaServer(t, `{"snapshots":[`+snapshot+`],"pagination":{"next":null}}`).ListSnapshots(ctx, "sb")
	r99toolsWantInvalid(t, "ListSnapshots pagination", err, "")
	// SessionsResponse and SessionResponse.
	if err := r99toolsSchemaServer(t, `{"sessions":[`+session+`],"pagination":{"count":1,"next":null}}`).eachSession(ctx, "sb", func(sessionWire) bool { return false }); err != nil {
		t.Errorf("valid listSessions: %v", err)
	}
	err = r99toolsSchemaServer(t, `{"sessions":[{"id":"s1"}],"pagination":{"count":1,"next":null}}`).eachSession(ctx, "sb", func(sessionWire) bool { return false })
	r99toolsWantInvalid(t, "listSessions", err, "")
	if _, err := r99toolsSchemaServer(t, `{"session":`+session+`}`).ExtendSessionTimeout(ctx, "s1", 1000); err != nil {
		t.Errorf("valid ExtendSessionTimeout: %v", err)
	}
	_, err = r99toolsSchemaServer(t, `{}`).ExtendSessionTimeout(ctx, "s1", 1000)
	r99toolsWantInvalid(t, "ExtendSessionTimeout empty", err, "")
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1 (extractSessionId)", apiErr.SessionID)
	}
	// CommandResponse / CommandFinishedResponse (exact text from zod).
	if _, err := r99toolsSchemaServer(t, `{"command":`+command+`}`).GetCommand(ctx, "s1", "c", false); err != nil {
		t.Errorf("valid GetCommand: %v", err)
	}
	_, err = r99toolsSchemaServer(t, `{"command":{"id":"c","name":"sh","args":[],"cwd":"/","sessionId":"s","startedAt":1}}`).GetCommand(ctx, "s1", "c", true)
	r99toolsWantInvalid(t, "GetCommand wait", err, "[\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"command\",\n      \"exitCode\"\n    ],\n    \"message\": \"Invalid input: expected number, received undefined\"\n  }\n]")
	err = r99toolsSchemaServer(t, `{"command":{"id":"c"}}`).KillCommand(ctx, "s1", "c", "SIGTERM")
	r99toolsWantInvalid(t, "KillCommand", err, "")
	// EmptyResponse: any object (an empty body reads as {}), not an array.
	if err := r99toolsSchemaServer(t, ``).MkDir(ctx, "s1", "d", "/"); err != nil {
		t.Errorf("valid MkDir: %v", err)
	}
	err = r99toolsSchemaServer(t, `[]`).MkDir(ctx, "s1", "d", "/")
	r99toolsWantInvalid(t, "MkDir array", err, "")
	// CommandStdinResponse.
	if n, err := parseStdinResponse(&httpinternal.Response{StatusCode: 200, Body: []byte(`{"command":` + command + `,"bytesWritten":3}`)}, false); err != nil || n == nil || *n != 3 {
		t.Errorf("valid stdin: %v, %v", n, err)
	}
	_, err = parseStdinResponse(&httpinternal.Response{StatusCode: 200, Body: []byte(`{"bytesWritten":3}`)}, false)
	r99toolsWantInvalid(t, "stdin without command", err, "")
	// The command stream records: CommandResponse.parse of the first,
	// CommandFinishedResponse.parse of the finished one (a ZodError).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"command":` + command + `}` + "\n" + `{"command":` + command + `}` + "\n"))
	}))
	defer srv.Close()
	_, err = NewAPIClient(srv.URL, Credentials{Token: "tok"}).RunCommandWait(ctx, "s1", runCommandRequest{Command: "sh"})
	if err == nil || !strings.Contains(err.Error(), `"exitCode"`) {
		t.Errorf("finished record with a null exitCode: err = %v", err)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"command":` + command + `}` + "\n" + `{"command":` + finished + `}` + "\n"))
	}))
	defer srv2.Close()
	if res, err := NewAPIClient(srv2.URL, Credentials{Token: "tok"}).RunCommandWait(ctx, "s1", runCommandRequest{Command: "sh"}); err != nil || res.Command.ExitCode == nil {
		t.Errorf("valid command stream: %+v, %v", res, err)
	}
}

// The V2 (request-shape) policy in a session response is rejected with the
// zod union text (node + zod 4).
func TestR99toolsResponsePolicyUnionText(t *testing.T) {
	issues := r99toolsUnionIssues()
	body := `{"session":{"id":"s1","memory":2048,"vcpus":1,"region":"iad1","timeout":300000,"status":"running","requestedAt":1,"createdAt":1,"cwd":"/","updatedAt":1,"networkPolicy":{"allow":["a"]}}}`
	_, err := r99toolsSchemaServer(t, body).ExtendSessionTimeout(context.Background(), "s1", 1)
	r99toolsWantInvalid(t, "V2 policy", err, issues)
}

func r99toolsUnionIssues() string {
	member := func(mode string) string {
		return "      [\n        {\n          \"code\": \"invalid_value\",\n          \"values\": [\n            \"" + mode + "\"\n          ],\n          \"path\": [\n            \"mode\"\n          ],\n          \"message\": \"Invalid input: expected \\\"" + mode + "\\\"\"\n        }\n      ]"
	}
	return "[\n  {\n    \"code\": \"invalid_union\",\n    \"errors\": [\n" + member("allow-all") + ",\n" + member("deny-all") + ",\n" + member("custom") + "\n    ],\n    \"path\": [\n      \"session\",\n      \"networkPolicy\"\n    ],\n    \"message\": \"Invalid input\"\n  }\n]"
}

// LogLine.parse: a record that does not match throws the ZodError (texts
// from node + zod 4: discriminatedUnion("stream", ...)).
func TestR99toolsLogLineSchema(t *testing.T) {
	for raw, want := range map[string]string{
		`{"stream":"stdin","data":"x"}`: "[\n  {\n    \"code\": \"invalid_union\",\n    \"errors\": [],\n    \"note\": \"No matching discriminator\",\n    \"discriminator\": \"stream\",\n    \"options\": [\n      \"stdout\",\n      \"stderr\",\n      \"error\"\n    ],\n    \"path\": [\n      \"stream\"\n    ],\n    \"message\": \"Invalid discriminator value. Expected 'stdout' | 'stderr' | 'error'\"\n  }\n]",
		`{"stream":"stdout","data":1}`:  "[\n  {\n    \"expected\": \"string\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"data\"\n    ],\n    \"message\": \"Invalid input: expected string, received number\"\n  }\n]",
		`"x"`:                           "[\n  {\n    \"code\": \"invalid_type\",\n    \"expected\": \"object\",\n    \"path\": [],\n    \"message\": \"Invalid input: expected object, received string\"\n  }\n]",
	} {
		if _, err := parseLogLine([]byte(raw), "s"); err == nil || err.Error() != want {
			t.Errorf("%s: err = %v\nwant %s", raw, err, want)
		}
	}
	if l, err := parseLogLine([]byte(`{"stream":"error","data":{"code":"c","message":"m"}}`), "s"); err != nil || l.Err == nil || l.Err.Code != "c" {
		t.Errorf("error record: %+v, %v", l, err)
	}
}

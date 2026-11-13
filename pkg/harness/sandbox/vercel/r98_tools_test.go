package vercel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// @vercel/sandbox 3.9.0 base-client.js parse() reads every API response with
// plain JSON.parse (no secureJsonParse), and jsonlines parses each ndjson
// line with plain JSON.parse: a __proto__ / constructor.prototype key is an
// ordinary property. Go's DoJSON and the nested decodes rejected it with a
// JSONParseError (review round 98; ruling 4).
func TestR98toolsAPIResponsesKeepProtoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"sandbox":{"name":"sbx","__proto__":{"x":1}},"session":{"id":"s1","networkPolicy":{"constructor":{"prototype":{}}}},"routes":[]}`))
		case r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"command":{"id":"cmd1","__proto__":{}}}` + "\n" +
				`{"stream":"stdout","data":"hi","__proto__":{}}` + "\n" +
				`{"command":{"id":"cmd1","exitCode":0},"__proto__":1}` + "\n"))
		}
	}))
	defer srv.Close()
	client := NewAPIClient(srv.URL, Credentials{Token: "tok"})
	out, err := client.GetSandbox(t.Context(), "sbx", nil)
	if err != nil || out.Sandbox.Name != "sbx" || out.Session.ID != "s1" {
		t.Fatalf("GetSandbox = %+v, %v", out, err)
	}
	result, err := client.RunCommandWait(t.Context(), "s1", runCommandRequest{Command: "echo"})
	if err != nil || result.Stdout != "hi" || result.Command.ExitCode == nil {
		t.Fatalf("RunCommandWait = %+v, %v", result, err)
	}
	// An error body is parsed the same way (extractServerMessage).
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","code":"c"},"__proto__":{}}`))
	}))
	defer errSrv.Close()
	_, err = NewAPIClient(errSrv.URL, Credentials{Token: "tok"}).GetSandbox(t.Context(), "sbx", nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Message != "bad" || apiErr.Code != "c" {
		t.Fatalf("error = %#v", err)
	}
}

// parse() reads an empty body as `JSON.parse(text || "{}")`: the
// EmptyResponse endpoints (mkdir, file writes) and the stdin write response
// accept it. Go failed with "unexpected end of JSON input" (review round 98).
func TestR98toolsEmptyBodyIsEmptyObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := NewAPIClient(srv.URL, Credentials{Token: "tok"})
	if err := client.MkDir(t.Context(), "s1", "dir", "/"); err != nil {
		t.Fatalf("MkDir: %v", err)
	}
	if err := client.WriteFiles(t.Context(), "s1", "/", []FileToWrite{{Name: "a.txt", Content: []byte("x")}}); err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	n, err := parseStdinResponse(&httpinternal.Response{StatusCode: 200}, false)
	if err != nil || n != nil {
		t.Fatalf("parseStdinResponse(empty) = %v, %v", n, err)
	}
	// Other endpoints still fail on an empty body (TS: the response schema
	// rejects {}; Go: the decode fails).
	if _, err := client.GetSandbox(t.Context(), "sbx", nil); err == nil {
		t.Fatal("GetSandbox accepted an empty body")
	}
}

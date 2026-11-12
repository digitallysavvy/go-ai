package vercel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// core3-08 (undici fetch httpRedirectFetch): a cross-origin redirect hop
// drops Authorization, Proxy-Authorization and Cookie.
func TestR91toolsVercelRedirectDropsCredentialHeaders(t *testing.T) {
	var got http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	client := &http.Client{CheckRedirect: dropCredentialsOnCrossOriginRedirect}
	req, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Proxy-Authorization", "Basic x")
	req.Header.Set("Cookie", "s=1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if got.Get(h) != "" {
			t.Errorf("%s reached the other origin", h)
		}
	}
}

// harness-02 (TS vercel-sandbox.test-d.ts:75, vercel-sandbox.ts:106,
// @vercel/sandbox 3.9.0 BaseCreateSandboxParams.source): a git source is
// sent as given, still gets the default runtime, and is hashed into the
// template name as TS does (expected name: node, sha256 of
// JSON.stringify([3, "recipe-git", {type, url, revision}])).
func TestR91toolsVercelGitSource(t *testing.T) {
	src := &SnapshotSource{Type: "git", URL: "https://github.com/o/r", Revision: "main"}
	params := withDefaultSandboxSettings(CreateParams{Source: src})
	if params.Runtime != DefaultSandboxRuntime {
		t.Fatalf("runtime = %q", params.Runtime)
	}
	body, _ := json.Marshal(createSandboxRequest{Ports: []int{}, Source: params.Source, Runtime: params.Runtime})
	if !strings.Contains(string(body), `"source":{"type":"git","url":"https://github.com/o/r","revision":"main"}`) || !strings.Contains(string(body), `"runtime":"node24"`) {
		t.Fatalf("body = %s", body)
	}
	if got := deriveTemplateName("recipe-git", params); got != "ai-sdk-harness-v3-22ca517f39416a12701e22a9" {
		t.Fatalf("template name = %s", got)
	}
	depth := 1
	tarball, _ := json.Marshal(SnapshotSource{Type: "git", URL: "u", Username: "x", Password: "p", Depth: &depth})
	if string(tarball) != `{"type":"git","url":"u","username":"x","password":"p","depth":1}` {
		t.Fatalf("private git source = %s", tarball)
	}
}

// Step-5 extra (3.9.0 CommandStdinResponse bytesWritten:
// z.number().int().nonnegative().optional()): null and integers above
// 2^53-1 fail like the zod schema.
func TestR91toolsVercelStdinBytesWrittenSchema(t *testing.T) {
	for body, wantErr := range map[string]bool{
		`{"bytesWritten":5}`:                false,
		`{}`:                                false,
		`{"bytesWritten":null}`:             true,
		`{"bytesWritten":9007199254740992}`: true,
		`{"bytesWritten":-1}`:               true,
		`{"bytesWritten":1.5}`:              true,
	} {
		_, err := parseStdinResponse(&httpinternal.Response{StatusCode: 200, Body: []byte(body)}, false)
		if (err != nil) != wantErr {
			t.Errorf("%s: err = %v", body, err)
		}
	}
}

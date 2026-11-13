package mcp

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// r98toolsModernJSONServer is a modern-era Streamable HTTP server that
// answers every POST with application/json (TS reads it with
// response.json(), no secureJsonParse). results maps a method to its result
// JSON; a function value builds the result from the request.
func r98toolsModernJSONServer(t *testing.T, results map[string]func(params json.RawMessage) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result string
		switch req.Method {
		case "server/discover":
			result = `{"resultType":"complete","supportedVersions":["` + LatestProtocolVersion + `"],"capabilities":{"tools":{}}}`
		default:
			build, ok := results[req.Method]
			if !ok {
				t.Errorf("unexpected method %q", req.Method)
				return
			}
			result = build(req.Params)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(req.ID)+`,"result":`+result+`}`)
	}))
}

// TS requestOnce reads response.result.resultType on the parsed object, and
// an application/json POST response is plain response.json(): a __proto__
// key anywhere in a modern result is an ordinary property. Go's resultType
// probe (validateModernResult) and the input_required check ran jsonexact's
// prototype check, so the call failed with "Modern MCP result is missing
// resultType" (review round 98).
func TestR98toolsModernJSONResultWithProtoKey(t *testing.T) {
	server := r98toolsModernJSONServer(t, map[string]func(json.RawMessage) string{
		"tools/call": func(json.RawMessage) string {
			return `{"resultType":"complete","content":[{"type":"text","text":"ok"}],"structuredContent":{"__proto__":{"x":1}}}`
		},
	})
	defer server.Close()
	client := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: server.URL}), MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	result, err := client.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("result = %#v", result)
	}
}

// TS InputRequiredResultSchema.parse / InputRequiredElicitationSchema.parse
// run on the parsed object: an input request whose params carry a
// __proto__ key (in requestedSchema) is answered. Go rejected it in
// isInputRequiredResult, parseInputRequiredResult and
// parseElicitationRequestParams (review round 98).
func TestR98toolsInputRequiredWithProtoKey(t *testing.T) {
	var mu sync.Mutex
	rounds := 0
	server := r98toolsModernJSONServer(t, map[string]func(json.RawMessage) string{
		"tools/call": func(params json.RawMessage) string {
			mu.Lock()
			defer mu.Unlock()
			rounds++
			if strings.Contains(string(params), `"inputResponses"`) {
				return `{"resultType":"complete","content":[{"type":"text","text":"done"}]}`
			}
			return `{"resultType":"input_required","inputRequests":{"a":{"method":"elicitation/create","params":{"message":"m","requestedSchema":{"type":"object","properties":{"__proto__":{"type":"string"}}}}}}}`
		},
	})
	defer server.Close()
	client := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: server.URL}), MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	var seen ElicitationRequest
	client.OnElicitationRequest(func(_ context.Context, req ElicitationRequest) (ElicitResult, error) {
		seen = req
		return ElicitResult{Action: "accept", Content: map[string]interface{}{"__proto__": "v"}}, nil
	})
	result, err := client.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "done" || seen.Message != "m" {
		t.Fatalf("result = %#v, request = %#v", result, seen)
	}
	mu.Lock()
	defer mu.Unlock()
	if rounds != 2 {
		t.Fatalf("rounds = %d", rounds)
	}
}

// TS ElicitResultSchema.parse(answer) runs on the caller's own object: a
// __proto__ key in content is an ordinary property, which
// zodlite.ValidationMessage's prototype check rejected (review round 98).
// A NaN / +-Inf value passes z.unknown() and is sent as null (ruling 4;
// already so, kept as a guard).
func TestR98toolsElicitResultNonFiniteAndProtoContent(t *testing.T) {
	var mu sync.Mutex
	var retried string
	server := r98toolsModernJSONServer(t, map[string]func(json.RawMessage) string{
		"tools/call": func(params json.RawMessage) string {
			mu.Lock()
			defer mu.Unlock()
			if strings.Contains(string(params), `"inputResponses"`) {
				retried = string(params)
				return `{"resultType":"complete","content":[]}`
			}
			return `{"resultType":"input_required","inputRequests":{"a":{"method":"elicitation/create","params":{"message":"m","requestedSchema":{"type":"object"}}}}}`
		},
	})
	defer server.Close()
	client := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: server.URL}), MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	client.OnElicitationRequest(func(context.Context, ElicitationRequest) (ElicitResult, error) {
		return ElicitResult{Action: "accept", Content: map[string]interface{}{"n": math.Inf(1), "__proto__": "v"}}, nil
	})
	if _, err := client.CallTool(context.Background(), "search", nil); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(retried, `"inputResponses":{"a":{"action":"accept","content":{"__proto__":"v","n":null}}}`) {
		t.Fatalf("retry params = %s", retried)
	}

	// The server-request path (onRequestMessage) validates the same way.
	encoded, err := jsonstr.Marshal(ElicitResult{Action: "accept", Content: map[string]interface{}{"n": math.NaN(), "__proto__": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if message, _ := zodIssues(elicitResultSchema, encoded); message != "" {
		t.Fatalf("zodIssues = %s", message)
	}
}

// zod copies a passthrough value as parsed: a nested __proto__ key in an
// undeclared tool member (an application/json tools/list body) stays in
// Extra. decodeLooseExtra dropped the member (review round 98).
func TestR98toolsLooseExtraKeepsNestedProtoKey(t *testing.T) {
	var tool MCPTool
	if err := unmarshalPlainJSON([]byte(`{"name":"t","inputSchema":{"type":"object"},"execution":{"__proto__":{"a":1}}}`), &tool); err != nil {
		t.Fatal(err)
	}
	exec, ok := tool.Extra["execution"].(map[string]interface{})
	if !ok || exec["__proto__"] == nil {
		t.Fatalf("Extra = %#v", tool.Extra)
	}
	// The secure entry decoder still rejects it.
	if err := unmarshalSafeJSON([]byte(`{"name":"t","inputSchema":{},"execution":{"__proto__":{}}}`), &tool); err == nil {
		t.Fatal("secure decode accepted __proto__")
	}
}

// TS discoverMetadataWithFallback / discoverAuthorizationServerMetadata
// default MCP-Protocol-Version to LATEST_PROTOCOL_VERSION (oauth.ts 545 /
// 706; oauth.test.ts expects it on every discovery GET), and auth() passes
// none. Go defaulted to the legacy ProtocolVersion alias (2025-11-25), so
// every OAuth discovery request of an authenticated MCP connection carried
// the wrong version (review round 98).
func TestR98toolsOAuthDiscoveryDefaultProtocolVersion(t *testing.T) {
	var mu sync.Mutex
	var versions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		versions = append(versions, r.URL.Path+" "+r.Header.Get("MCP-Protocol-Version"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_, _ = io.WriteString(w, `{"resource":"`+"http://"+r.Host+`","authorization_servers":["http://`+r.Host+`"]}`)
		case "/.well-known/oauth-authorization-server":
			_, _ = io.WriteString(w, `{"issuer":"http://`+r.Host+`","authorization_endpoint":"http://`+r.Host+`/authorize","token_endpoint":"http://`+r.Host+`/token","response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	if _, err := DiscoverOAuthProtectedResourceMetadata(context.Background(), server.URL, OAuthDiscoveryOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverAuthorizationServerMetadata(context.Background(), server.URL, OAuthDiscoveryOptions{TrustedOrigin: server.URL}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"/.well-known/oauth-protected-resource 2026-07-28",
		"/.well-known/oauth-authorization-server 2026-07-28",
	}
	if strings.Join(versions, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %q, want %q", versions, want)
	}
}

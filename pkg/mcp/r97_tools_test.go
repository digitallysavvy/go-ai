package mcp

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TS getStandardRequestHeaders reads params.name from the client's own
// request object: a tool argument named __proto__ (a JS own property after
// JSON.parse elsewhere, no secureJsonParse on outbound params) leaves the
// Mcp-Name header in place. Go read the params through jsonexact, whose
// prototype check dropped the header (review round 97).
func TestR97toolsStandardRequestHeaderNameWithProtoArgument(t *testing.T) {
	call, _ := CreateRequest(1, "tools/call", map[string]interface{}{
		"name":      "search",
		"arguments": map[string]interface{}{"__proto__": map[string]interface{}{"x": 1}},
	})
	h := postAndCaptureHeaders(t, LatestProtocolVersion, nil, call)
	if h.Get("Mcp-Name") != "search" {
		t.Fatalf("Mcp-Name = %q, want search", h.Get("Mcp-Name"))
	}
	read, _ := CreateRequest(2, "resources/read", map[string]interface{}{"uri": "ui://x", "_meta": map[string]interface{}{"__proto__": 1}})
	h = postAndCaptureHeaders(t, LatestProtocolVersion, nil, read)
	if h.Get("Mcp-Name") != "ui://x" {
		t.Fatalf("Mcp-Name = %q, want ui://x", h.Get("Mcp-Name"))
	}
	// A non-string name still gives no header (typeof name === 'string').
	odd, _ := CreateRequest(3, "prompts/get", map[string]interface{}{"name": 5})
	h = postAndCaptureHeaders(t, LatestProtocolVersion, nil, odd)
	if h.Get("Mcp-Name") != "" {
		t.Fatalf("Mcp-Name = %q, want none", h.Get("Mcp-Name"))
	}
}

// The SDK's own MarshalJSON methods write like JSON.stringify (ruling 4 and
// the jsonstr class): a non-finite number is null instead of failing the
// whole value, and <, > and & stay literal on the wire (prompts/get
// arguments and completion values went out escaped, review round 97).
func TestR97toolsMarshalJSONLikeJSONStringify(t *testing.T) {
	b, err := json.Marshal(MCPAppResource{URI: "ui://a", MimeType: MCPAppMimeType, HTML: "<p>", Meta: map[string]interface{}{"n": math.Inf(1)}})
	if err != nil || !strings.Contains(string(b), `"meta":{"n":null}`) {
		t.Fatalf("MCPAppResource = %s, %v", b, err)
	}
	b, err = json.Marshal(MCPAppResource{URI: "ui://a", MimeType: MCPAppMimeType, HTML: "x", Meta: map[string]interface{}{}})
	if err != nil || string(b) != `{"uri":"ui://a","mimeType":"text/html;profile=mcp-app","html":"x","meta":{}}` {
		t.Fatalf("empty meta = %s, %v", b, err)
	}
	msg, err := CreateRequest(1, "prompts/get", GetPromptParams{Name: "p", Arguments: map[string]interface{}{"q": "<a&b>", "n": math.NaN()}})
	if err != nil || string(msg.Params) != `{"name":"p","arguments":{"n":null,"q":"<a&b>"}}` {
		t.Fatalf("prompts/get params = %s, %v", msg.Params, err)
	}
	if _, err := json.Marshal(GetPromptParams{Name: "p", Arguments: map[string]interface{}{"n": math.Inf(-1)}}); err != nil {
		t.Fatalf("GetPromptParams marshal: %v", err)
	}
	msg, err = CreateRequest(2, "completion/complete", CompleteRequestParams{
		PromptRef: &PromptReference{Type: "ref/prompt", Name: "p"},
		Argument:  CompletionArgument{Name: "a", Value: "<x>"},
	})
	if err != nil || string(msg.Params) != `{"ref":{"type":"ref/prompt","name":"p"},"argument":{"name":"a","value":"<x>"}}` {
		t.Fatalf("completion params = %s, %v", msg.Params, err)
	}
}

// TS ElicitationRequestSchema (zod 4) names a non-finite number by its value:
// "received Infinity" for message: 1e400 (Go said "received number";
// ruling 4, review round 97).
func TestR97toolsElicitationIssueNonFinite(t *testing.T) {
	_, msg, _ := parseElicitationRequestParams(json.RawMessage(`{"message":1e400,"requestedSchema":{}}`))
	if !strings.Contains(msg, "expected string, received Infinity") {
		t.Fatalf("message = %s", msg)
	}
}

// TS reads an application/json POST response with response.json() (plain
// JSON.parse, mcp-http-transport.ts 390): a __proto__ key is an ordinary
// member and the message is delivered. The event-stream, SSE and stdio
// paths use parseJSONRPCMessage (secure parseJSON) and keep rejecting it
// (review round 97, coordinator class from provg).
func TestR97toolsHTTPJSONResponseAcceptsProtoKey(t *testing.T) {
	single := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[],"__proto__":{"x":1}}}`)
	msgs, err := parseMCPHTTPJSONMessages(single)
	if err != nil || len(msgs) != 1 || msgs[0].ID != float64(1) {
		t.Fatalf("single: %v, %#v", err, msgs)
	}
	batch := []byte(`[{"jsonrpc":"2.0","id":2,"result":{"constructor":{"prototype":{}}}}]`)
	if msgs, err := parseMCPHTTPJSONMessages(batch); err != nil || len(msgs) != 1 {
		t.Fatalf("batch: %v, %#v", err, msgs)
	}
	if _, err := parseMCPMessage(single); err == nil {
		t.Fatal("event-stream path accepted a __proto__ key")
	}
	// The Go memory guards still apply on the plain path.
	deep := `{"jsonrpc":"2.0","id":1,"result":` + strings.Repeat(`{"a":`, 70) + `1` + strings.Repeat(`}`, 70) + `}`
	if _, err := parseMCPHTTPJSONMessages([]byte(deep)); err == nil || !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("deep: %v", err)
	}
	// The result re-read after the transport (callOnce, the input_required
	// loop) applies only the limits; its jsonexact decode is the CR'd swap.
	if err := checkServerJSON([]byte(`{"__proto__":1}`), false); err != nil {
		t.Fatalf("plain check: %v", err)
	}
	if err := checkServerJSON([]byte(`{"__proto__":1}`), true); err == nil {
		t.Fatal("secure check accepted __proto__")
	}
}

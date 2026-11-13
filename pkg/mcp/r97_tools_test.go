package mcp

import "testing"

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

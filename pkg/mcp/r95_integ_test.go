package mcp

import "testing"

// TS getStandardRequestHeaders reads params.name / params.uri by their exact
// names: a key that differs only in case gives no Mcp-Name header (review
// round 95, guard extension to pkg/mcp).
func TestR95integStandardRequestHeaderNameKeyIsExact(t *testing.T) {
	call, _ := CreateRequest(1, "tools/call", map[string]interface{}{"Name": "search", "arguments": map[string]interface{}{}})
	h := postAndCaptureHeaders(t, LatestProtocolVersion, nil, call)
	if h.Get("Mcp-Method") != "tools/call" || h.Get("Mcp-Name") != "" {
		t.Fatalf("headers = %v, want Mcp-Method only", h)
	}
	read, _ := CreateRequest(2, "resources/read", map[string]interface{}{"URI": "ui://x"})
	h = postAndCaptureHeaders(t, LatestProtocolVersion, nil, read)
	if h.Get("Mcp-Name") != "" {
		t.Fatalf("headers = %v, want no Mcp-Name", h)
	}
}

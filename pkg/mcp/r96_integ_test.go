package mcp

import "testing"

// carry CR §2 (tools site, round 96): CallToolResultWithStructuredContentSchema
// has structuredContent: z.json(). Without content, a non-finite number in
// it (JSON.parse of 1e400) fails that arm, and with no toolResult the whole
// result fails; with content, structuredContent is z.unknown() and passes.
func TestR96integStructuredContentJSONValue(t *testing.T) {
	var r CallToolResult
	if err := unmarshalSafeJSON([]byte(`{"structuredContent":{"v":[1,"a"]}}`), &r); err != nil || len(r.Content) != 1 {
		t.Fatalf("finite: err = %v, content = %+v", err, r.Content)
	}
	if err := unmarshalSafeJSON([]byte(`{"structuredContent":{"v":1e400}}`), &r); err == nil {
		t.Fatal("1e400 without content: err = nil, want the z.json() failure")
	}
	if err := unmarshalSafeJSON([]byte(`{"content":[],"structuredContent":{"v":1e400}}`), &r); err != nil {
		t.Fatalf("1e400 with content: err = %v", err)
	}
	if err := unmarshalSafeJSON([]byte(`{"structuredContent":{"v":1e400},"toolResult":1}`), &r); err != nil || r.ToolResult != float64(1) {
		t.Fatalf("1e400 with toolResult: err = %v, toolResult = %#v", err, r.ToolResult)
	}
}

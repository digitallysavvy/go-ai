package mcp

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The text a TS client shows the model is JSON.stringify of the zod-parsed
// value: schema keys first, then the extras in wire order, no HTML escaping.
func TestStructuredContentTextIsJSONStringify(t *testing.T) {
	var result CallToolResult
	if err := json.Unmarshal([]byte(`{"structuredContent":{"b":1.0,"a":"<x>&","n":[1e2,0.5]}}`), &result); err != nil {
		t.Fatal(err)
	}
	if got, want := result.Content[0].Text, `{"b":1,"a":"<x>&","n":[100,0.5]}`; got != want {
		t.Fatalf("text = %s, want %s", got, want)
	}
}

func TestContentBlockModelOutputFollowsZodKeyOrder(t *testing.T) {
	client := NewMCPClient(newMockTransport(), MCPClientConfig{})
	converter := NewMCPToolConverter(client)
	var result CallToolResult
	raw := `{"content":[` +
		`{"mimeType":"text/html","name":"n","uri":"file:///<","type":"resource_link","zz":{"b":1,"a":2}},` +
		`{"b":1,"type":"custom","a":2}]}`
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	out := converter.convertToModelOutput(result)
	want := []string{
		`{"type":"resource_link","uri":"file:///<","name":"n","mimeType":"text/html","zz":{"b":1,"a":2}}`,
		`{"type":"custom","b":1,"a":2}`,
	}
	for i, w := range want {
		block, ok := out.Content[i].(types.TextContentBlock)
		if !ok || block.Text != w {
			t.Fatalf("block %d = %#v, want text %s", i, out.Content[i], w)
		}
	}
}

func TestCallToolResultKeepsZodKeyOrder(t *testing.T) {
	var result CallToolResult
	if err := json.Unmarshal([]byte(`{"extra":1,"isError":true,"structuredContent":{"k":1},"_meta":{"m":1},"content":[],"resultType":"complete"}`), &result); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"_meta":{"m":1},"resultType":"complete","content":[],"structuredContent":{"k":1},"isError":true,"extra":1}`; string(out) != want {
		t.Fatalf("json = %s\nwant   %s", out, want)
	}
}

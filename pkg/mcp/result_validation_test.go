package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Mirrors the TS result schemas (types.ts): a result missing a required field
// fails with "Failed to parse server response" rather than decoding into zero
// values (a tool with an empty name, a resource with no uri).
func TestValidateMCPResultShape(t *testing.T) {
	cases := []struct {
		name, method, result string
		want                 string // empty means valid
	}{
		{"tools ok", "tools/list", `{"tools":[{"name":"a","inputSchema":{"type":"object"}}],"resultType":"complete"}`, ""},
		{"tools empty", "tools/list", `{"tools":[]}`, ""},
		{"tools missing array", "tools/list", `{}`, "tools must be an array"},
		{"tool missing name", "tools/list", `{"tools":[{"inputSchema":{}}]}`, "tools[0]: name must be a string"},
		{"tool missing inputSchema", "tools/list", `{"tools":[{"name":"a"}]}`, "inputSchema must be an object"},
		{"tool bad properties", "tools/list", `{"tools":[{"name":"a","inputSchema":{"properties":[]}}]}`, "properties must be an object"},
		{"resource missing uri", "resources/list", `{"resources":[{"name":"n"}]}`, "uri must be a string"},
		{"template missing name", "resources/templates/list", `{"resourceTemplates":[{"uriTemplate":"x://{a}"}]}`, "name must be a string"},
		{"read ok text", "resources/read", `{"contents":[{"uri":"u","text":""}]}`, ""},
		{"read ok blob", "resources/read", `{"contents":[{"uri":"u","blob":"AA=="}]}`, ""},
		{"read neither", "resources/read", `{"contents":[{"uri":"u"}]}`, "text or blob"},
		{"prompt missing name", "prompts/list", `{"prompts":[{}]}`, "name must be a string"},
		{"prompt message bad role", "prompts/get", `{"messages":[{"role":"system","content":{"type":"text","text":"x"}}]}`, "role must be"},
		{"prompt message ok", "prompts/get", `{"messages":[{"role":"user","content":{"type":"text","text":"x"}}]}`, ""},
		{"complete missing values", "completion/complete", `{"completion":{}}`, "completion.values must be an array"},
		{"initialize missing serverInfo", "initialize", `{"protocolVersion":"2025-11-25","capabilities":{}}`, "serverInfo must be an object"},
		{"initialize ok", "initialize", `{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`, ""},
		{"discover missing versions", "server/discover", `{"capabilities":{}}`, "supportedVersions must be an array"},
		{"tool description number", "tools/list", `{"tools":[{"name":"a","description":5,"inputSchema":{}}]}`, "description must be a string"},
		{"tool annotation hint", "tools/list", `{"tools":[{"name":"a","annotations":{"readOnlyHint":"y"},"inputSchema":{}}]}`, "readOnlyHint must be a boolean"},
		{"bad nextCursor", "tools/list", `{"tools":[],"nextCursor":1}`, "nextCursor must be a string"},
		{"bad _meta", "tools/list", `{"tools":[],"_meta":[]}`, "_meta must be an object"},
		{"resource size", "resources/list", `{"resources":[{"uri":"u","name":"n","size":"1"}]}`, "size must be a number"},
		{"caps tools bool", "initialize", `{"protocolVersion":"x","capabilities":{"tools":{"listChanged":"y"}},"serverInfo":{"name":"s","version":"1"}}`, "tools.listChanged must be a boolean"},
		{"discover scope", "server/discover", `{"supportedVersions":[],"capabilities":{},"cacheScope":"x"}`, "cacheScope"},
		{"complete total", "completion/complete", `{"completion":{"values":[],"total":1.5}}`, "total must be an integer"},
		{"call bad _meta", "tools/call", `{"content":[],"_meta":5}`, "_meta must be an object"},
		{"call bad resultType", "tools/call", `{"content":[],"resultType":1}`, "resultType must be a string"},
		{"call ok", "tools/call", `{"content":[],"_meta":{}}`, ""},
		{"unvalidated method", "ping", `{}`, ""},
		{"not an object", "tools/list", `[]`, "must be an object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMCPResultShape(tc.method, json.RawMessage(tc.result))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var mcpErr *MCPClientError
			if !errors.As(err, &mcpErr) || mcpErr.Message != "Failed to parse server response" {
				t.Fatalf("err = %v, want MCPClientError Failed to parse server response", err)
			}
			if mcpErr.Cause == nil || !strings.Contains(mcpErr.Cause.Error(), tc.want) {
				t.Fatalf("cause = %v, want it to contain %q", mcpErr.Cause, tc.want)
			}
		})
	}
}

func TestClientListToolsRejectsToolWithoutInputSchema(t *testing.T) {
	transport := newHeaderCapableTransport([]MCPTool{{Name: "broken"}}) // inputSchema marshals as null
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	_, err := client.ListTools(t.Context())
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "Failed to parse server response" {
		t.Fatalf("err = %v, want Failed to parse server response", err)
	}
}

func TestValidateMCPResultShapeR18(t *testing.T) {
	bad := map[string]string{
		"resources/read": `{"contents":[{"uri":"u","blob":"not base64!"}]}`,
		"prompts/get":    `{"messages":[{"role":"user","content":{"type":"audio","data":"x"}}]}`,
	}
	for method, body := range bad {
		if err := validateMCPResultShape(method, json.RawMessage(body)); err == nil {
			t.Errorf("%s: expected error for %s", method, body)
		}
	}
	good := map[string]string{
		"resources/read": `{"contents":[{"uri":"u","blob":"aGk="}]}`,
		"prompts/get":    `{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}]}`,
	}
	for method, body := range good {
		if err := validateMCPResultShape(method, json.RawMessage(body)); err != nil {
			t.Errorf("%s: unexpected error %v", method, err)
		}
	}
	if isBase64String("aGk=\n") {
		t.Error("zod z.base64() rejects newlines")
	}
}

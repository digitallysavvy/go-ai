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
		{"unvalidated method", "tools/call", `{}`, ""},
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

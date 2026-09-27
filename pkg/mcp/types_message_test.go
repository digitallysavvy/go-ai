package mcp

import (
	"encoding/json"
	"testing"
)

func TestMCPMessageJSONShapes(t *testing.T) {
	request := MCPMessage{
		JSONRpc: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  json.RawMessage(`{"cursor":"abc"}`),
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request error = %v", err)
	}
	if string(data) != `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"abc"}}` {
		t.Fatalf("request JSON = %s", string(data))
	}

	response := MCPMessage{
		JSONRpc: "2.0",
		ID:      "req-1",
		Result:  json.RawMessage(`{"tools":[]}`),
	}
	data, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response error = %v", err)
	}
	if string(data) != `{"jsonrpc":"2.0","id":"req-1","result":{"tools":[]}}` {
		t.Fatalf("response JSON = %s", string(data))
	}
}

func TestMCPInitializeAndListTypesRoundTrip(t *testing.T) {
	initResult := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ServerCapabilities{Tools: &ToolsCapability{ListChanged: true}},
		ServerInfo:      ServerInfo{Name: "test-server", Version: "1.2.3"},
		Instructions:    "follow steps",
	}
	data, err := json.Marshal(initResult)
	if err != nil {
		t.Fatalf("marshal init result error = %v", err)
	}
	var decoded InitializeResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal init result error = %v", err)
	}
	if decoded.ProtocolVersion != ProtocolVersion || decoded.ServerInfo.Name != "test-server" || decoded.Capabilities.Tools == nil || !decoded.Capabilities.Tools.ListChanged {
		t.Fatalf("decoded init result mismatch: %#v", decoded)
	}

	listResult := ListToolsResult{
		Tools: []MCPTool{
			{Name: "search", Description: "search docs", InputSchema: map[string]interface{}{"type": "object"}},
		},
		NextCursor: "next",
	}
	data, err = json.Marshal(listResult)
	if err != nil {
		t.Fatalf("marshal list result error = %v", err)
	}
	var decodedList ListToolsResult
	if err := json.Unmarshal(data, &decodedList); err != nil {
		t.Fatalf("unmarshal list result error = %v", err)
	}
	if len(decodedList.Tools) != 1 || decodedList.Tools[0].Name != "search" || decodedList.NextCursor != "next" {
		t.Fatalf("decoded list result mismatch: %#v", decodedList)
	}
}

// TestCallToolResultSynthesizesTextFromStructuredContentOnly mirrors TS
// CallToolResultWithStructuredContentSchema (types.ts, hash 3da84fd /
// types.test.ts "normalizes structured-only results with %s content"): a
// result carrying only structuredContent (no content field) gets a
// synthesized single text content block and isError defaults to false.
func TestCallToolResultSynthesizesTextFromStructuredContentOnly(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"object", `{"structuredContent":{"value":42}}`, `{"value":42}`},
		{"array", `{"structuredContent":[1,"two",false]}`, `[1,"two",false]`},
		{"string", `{"structuredContent":"result"}`, `"result"`},
		{"number", `{"structuredContent":42}`, `42`},
		{"boolean", `{"structuredContent":true}`, `true`},
		{"null", `{"structuredContent":null}`, `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result CallToolResult
			if err := json.Unmarshal([]byte(tc.json), &result); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if result.IsError {
				t.Fatalf("IsError = true, want false (default)")
			}
			if len(result.Content) != 1 {
				t.Fatalf("Content = %#v, want a single synthesized text block", result.Content)
			}
			block := result.Content[0]
			if block.Type != "text" || block.Text != tc.want {
				t.Fatalf("synthesized block = %#v, want type=text text=%s", block, tc.want)
			}
		})
	}
}

// TestCallToolResultStructuredOnlyPreservesIsError mirrors TS types.test.ts
// "preserves structured-only error results".
func TestCallToolResultStructuredOnlyPreservesIsError(t *testing.T) {
	var result CallToolResult
	if err := json.Unmarshal([]byte(`{"structuredContent":{"code":"NOT_FOUND"},"isError":true}`), &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true (preserved)")
	}
	if len(result.Content) != 1 || result.Content[0].Text != `{"code":"NOT_FOUND"}` {
		t.Fatalf("Content = %#v", result.Content)
	}
}

// TestCallToolResultWithContentDoesNotSynthesize mirrors TS types.test.ts
// "preserves results that already contain content": when content is already
// present, structuredContent must not trigger synthesis/override.
func TestCallToolResultWithContentDoesNotSynthesize(t *testing.T) {
	var result CallToolResult
	raw := `{"content":[{"type":"text","text":"Existing content"}],"structuredContent":{"value":42}}`
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Existing content" {
		t.Fatalf("Content = %#v, want the original content preserved", result.Content)
	}
}

// TestServerInfoTitleRoundTrip mirrors TS Configuration.title addition
// (mcp-client.ts/types.ts, hash a98bf66).
func TestServerInfoTitleRoundTrip(t *testing.T) {
	info := ServerInfo{Name: "filesystem", Title: "Filesystem Server", Version: "1.0.0"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(data) != `{"name":"filesystem","title":"Filesystem Server","version":"1.0.0"}` {
		t.Fatalf("ServerInfo JSON = %s", string(data))
	}
	var decoded ServerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Title != "Filesystem Server" {
		t.Fatalf("decoded title = %q", decoded.Title)
	}
}

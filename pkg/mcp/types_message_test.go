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

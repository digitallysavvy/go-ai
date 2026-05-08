package mcp

import "testing"

func TestUnmarshalSafeJSONRejectsUnsafeKeys(t *testing.T) {
	var target map[string]interface{}
	err := unmarshalSafeJSON([]byte(`{"params":{"__proto__":{"polluted":true}}}`), &target)
	if err == nil {
		t.Fatal("expected unsafe key error")
	}
}

func TestParseResultUsesSafeJSON(t *testing.T) {
	msg := &MCPMessage{Result: []byte(`{"tools":[{"name":"bad","inputSchema":{"constructor":{}}}]}`)}
	var result ListToolsResult
	err := ParseResult(msg, &result)
	if err == nil {
		t.Fatal("expected unsafe key error")
	}
}

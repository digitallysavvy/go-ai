package mcp

import (
	"fmt"
	"strings"
	"testing"
)

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

func TestUnmarshalSafeJSONRejectsExcessDepth(t *testing.T) {
	data := strings.Repeat(`{"a":`, defaultJSONMaxDepth+2) + `1` + strings.Repeat(`}`, defaultJSONMaxDepth+2)
	var target map[string]interface{}
	err := unmarshalSafeJSON([]byte(data), &target)
	if err == nil || !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("expected depth limit error, got %v", err)
	}
}

func TestUnmarshalSafeJSONRejectsExcessFields(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{`)
	for i := 0; i < defaultJSONMaxFields+1; i++ {
		if i > 0 {
			b.WriteString(`,`)
		}
		b.WriteString(fmt.Sprintf(`"k%d":1`, i))
	}
	b.WriteString(`}`)

	var target map[string]interface{}
	err := unmarshalSafeJSON([]byte(b.String()), &target)
	if err == nil || !strings.Contains(err.Error(), "field count") {
		t.Fatalf("expected field-count limit error, got %v", err)
	}
}

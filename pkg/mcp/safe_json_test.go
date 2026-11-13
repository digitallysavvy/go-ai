package mcp

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnmarshalSafeJSONRejectsProtoKey(t *testing.T) {
	var target map[string]interface{}
	err := unmarshalSafeJSON([]byte(`{"params":{"__proto__":{"polluted":true}}}`), &target)
	if err == nil || !strings.Contains(err.Error(), "forbidden prototype property") {
		t.Fatalf("expected forbidden prototype property error, got %v", err)
	}
}

func TestUnmarshalSafeJSONRejectsConstructorPrototype(t *testing.T) {
	var target map[string]interface{}
	err := unmarshalSafeJSON([]byte(`{"params":{"constructor":{"prototype":{"polluted":true}}}}`), &target)
	if err == nil || !strings.Contains(err.Error(), "forbidden prototype property") {
		t.Fatalf("expected forbidden prototype property error, got %v", err)
	}
}

func TestUnmarshalSafeJSONPreservesOrdinaryConstructorKey(t *testing.T) {
	var target map[string]interface{}
	err := unmarshalSafeJSON([]byte(`{"params":{"constructor":{"x":true}}}`), &target)
	if err != nil {
		t.Fatalf("expected ordinary constructor key to parse, got %v", err)
	}
	params, _ := target["params"].(map[string]interface{})
	if params == nil || params["constructor"] == nil {
		t.Fatalf("constructor key was not preserved: %#v", target)
	}
}

func TestParseResultPreservesPrototypeNamedSchemaKeys(t *testing.T) {
	msg := &MCPMessage{Result: []byte(`{"tools":[{"name":"bad","inputSchema":{"constructor":{}}}]}`)}
	var result ListToolsResult
	err := ParseResult(msg, &result)
	if err != nil {
		t.Fatalf("expected schema key to parse as ordinary Go map key, got %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(result.Tools))
	}
	if _, ok := result.Tools[0].InputSchema["constructor"]; !ok {
		t.Fatalf("schema constructor key was not preserved: %#v", result.Tools[0].InputSchema)
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

func TestRejectUnsafeJSONFieldCountLimit(t *testing.T) {
	value := map[string]interface{}{"a": 1, "b": 2, "c": 3}
	if err := rejectUnsafeJSON(value, 8, 2); err == nil || !strings.Contains(err.Error(), "field count") {
		t.Fatalf("expected field-count limit error, got %v", err)
	}
	if err := rejectUnsafeJSON(value, 8, 3); err != nil {
		t.Fatal(err)
	}
}

// TS has no field limit: a legitimate result with many object members parses.
func TestUnmarshalSafeJSONAcceptsLargeLegitimateResult(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; i < 3000; i++ {
		if i > 0 {
			b.WriteString(`,`)
		}
		_, _ = fmt.Fprintf(&b, `{"id":%d,"a":1,"b":2}`, i)
	}
	b.WriteString(`]}`)
	var target map[string]interface{}
	if err := unmarshalSafeJSON([]byte(b.String()), &target); err != nil {
		t.Fatalf("large result rejected: %v", err)
	}
}

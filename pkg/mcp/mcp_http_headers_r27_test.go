package mcp

import "testing"

// With several violations the first one reported must not depend on Go's map
// iteration order.
func TestGetMCPToolHeaderBindingsErrorIsDeterministic(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "object", "x-mcp-header": "bad name"},
			"b": map[string]interface{}{"type": "string", "x-mcp-header": "also bad"},
			"c": map[string]interface{}{"type": "number", "x-mcp-header": "Ok"},
			"d": map[string]interface{}{"type": "string", "x-mcp-header": "!!"},
		},
	}
	_, first := GetMCPToolHeaderBindings(schema)
	if first == nil {
		t.Fatal("expected an error")
	}
	for i := 0; i < 200; i++ {
		if _, err := GetMCPToolHeaderBindings(schema); err == nil || err.Error() != first.Error() {
			t.Fatalf("run %d: %v, want %v", i, err, first)
		}
	}
}

package mcp

import (
	"sort"
	"strings"
	"testing"
)

// TestGetMCPToolHeaderBindingsExtractsStaticallyReachable mirrors TS
// mcp-http-headers.test.ts "extracts statically reachable header bindings".
func TestGetMCPToolHeaderBindingsExtractsStaticallyReachable(t *testing.T) {
	bindings, err := GetMCPToolHeaderBindings(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"region": map[string]interface{}{"type": "string", "x-mcp-header": "Region"},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"dryRun": map[string]interface{}{"type": "boolean", "x-mcp-header": "Dry-Run"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings = %#v, want 2", bindings)
	}
	// Go's schema is an unordered map, so bindings are sorted by path (see
	// GetMCPToolHeaderBindings doc comment) rather than declaration order.
	sort.Slice(bindings, func(i, j int) bool { return strings.Join(bindings[i].Path, ".") < strings.Join(bindings[j].Path, ".") })

	want := []MCPToolHeaderBinding{
		{HeaderName: "Dry-Run", Path: []string{"options", "dryRun"}, ValueType: MCPHeaderValueBoolean},
		{HeaderName: "Region", Path: []string{"region"}, ValueType: MCPHeaderValueString},
	}
	for i, w := range want {
		got := bindings[i]
		if got.HeaderName != w.HeaderName || got.ValueType != w.ValueType || strings.Join(got.Path, ".") != strings.Join(w.Path, ".") {
			t.Fatalf("bindings[%d] = %#v, want %#v", i, got, w)
		}
	}
}

// TestGetMCPToolHeaderBindingsRejectsInvalidSchemas mirrors TS
// mcp-http-headers.test.ts "rejects invalid x-mcp-header schemas".
func TestGetMCPToolHeaderBindingsRejectsInvalidSchemas(t *testing.T) {
	cases := []struct {
		name    string
		schema  map[string]interface{}
		wantErr string
	}{
		{
			name: "items is not statically reachable",
			schema: map[string]interface{}{
				"type":  "object",
				"items": map[string]interface{}{"type": "string", "x-mcp-header": "Invalid"},
			},
			wantErr: "not on a statically reachable property",
		},
		{
			name: "array items.properties is not statically reachable",
			schema: map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"region": map[string]interface{}{"type": "string", "x-mcp-header": "Region"},
					},
				},
			},
			wantErr: "not on a statically reachable property",
		},
		{
			name: "additionalProperties is not statically reachable",
			schema: map[string]interface{}{
				"type": "object",
				"additionalProperties": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"region": map[string]interface{}{"type": "string", "x-mcp-header": "Region"},
					},
				},
			},
			wantErr: "not on a statically reachable property",
		},
		{
			name: "non boolean/integer/string type",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"count": map[string]interface{}{"type": "number", "x-mcp-header": "Count"},
				},
			},
			wantErr: "can only annotate boolean, integer, or string",
		},
		{
			name: "duplicate header name (case-insensitive)",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"first":  map[string]interface{}{"type": "string", "x-mcp-header": "Region"},
					"second": map[string]interface{}{"type": "string", "x-mcp-header": "region"},
				},
			},
			wantErr: "is not unique",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetMCPToolHeaderBindings(tc.schema)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestCreateMCPToolHeadersEncodesFromArguments mirrors TS
// mcp-http-headers.test.ts "creates encoded headers from tool arguments".
func TestCreateMCPToolHeadersEncodesFromArguments(t *testing.T) {
	bindings, err := GetMCPToolHeaderBindings(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"region": map[string]interface{}{"type": "string", "x-mcp-header": "Region"},
			"count":  map[string]interface{}{"type": "integer", "x-mcp-header": "Count"},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"enabled": map[string]interface{}{"type": "boolean", "x-mcp-header": "Enabled"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMCPToolHeaderBindings error: %v", err)
	}

	headers, err := CreateMCPToolHeaders(bindings, map[string]interface{}{
		"region": "Hello, 世界",
		"count":  float64(42), // json.Unmarshal into interface{} decodes numbers as float64
		"options": map[string]interface{}{
			"enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("CreateMCPToolHeaders error: %v", err)
	}

	want := map[string]string{
		"Mcp-Param-Region":  "=?base64?SGVsbG8sIOS4lueVjA==?=",
		"Mcp-Param-Count":   "42",
		"Mcp-Param-Enabled": "false",
	}
	if len(headers) != len(want) {
		t.Fatalf("headers = %#v, want %#v", headers, want)
	}
	for k, v := range want {
		if headers[k] != v {
			t.Fatalf("headers[%q] = %q, want %q", k, headers[k], v)
		}
	}
}

// TestEncodeMCPHeaderValue mirrors TS mcp-http-headers.test.ts "encodes MCP
// header values safely".
func TestEncodeMCPHeaderValue(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"plain-ascii", "plain-ascii"},
		{" padded ", "=?base64?IHBhZGRlZCA=?="},
		{"=?base64?literal?=", "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?="},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := EncodeMCPHeaderValue(tc.value); got != tc.want {
				t.Fatalf("EncodeMCPHeaderValue(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

package jsonparser

import (
	"encoding/json"
	"testing"
)

func TestFixJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "complete object",
			input:    `{"name":"John"}`,
			expected: `{"name":"John"}`,
		},
		{
			name:     "incomplete object - missing closing brace",
			input:    `{"name":"John"`,
			expected: `{"name":"John"}`,
		},
		{
			name:     "incomplete string",
			input:    `{"name":"Joh`,
			expected: `{"name":"Joh"}`,
		},
		{
			name:     "incomplete array",
			input:    `{"items":[1,2,3`,
			expected: `{"items":[1,2,3]}`,
		},
		{
			name:     "nested incomplete object",
			input:    `{"user":{"name":"John","age":30`,
			expected: `{"user":{"name":"John","age":30}}`,
		},
		{
			name:     "incomplete boolean literal - true",
			input:    `{"active":tr`,
			expected: `{"active":true}`,
		},
		{
			name:     "incomplete boolean literal - false",
			input:    `{"active":fal`,
			expected: `{"active":false}`,
		},
		{
			name:     "incomplete null literal",
			input:    `{"value":nul`,
			expected: `{"value":null}`,
		},
		{
			name:     "incomplete number",
			input:    `{"count":42`,
			expected: `{"count":42}`,
		},
		{
			name:     "incomplete decimal number",
			input:    `{"price":19.99`,
			expected: `{"price":19.99}`,
		},
		{
			name:     "array with incomplete last element",
			input:    `[1,2,{"key":"val`,
			expected: `[1,2,{"key":"val"}]`,
		},
		{
			name:     "deeply nested incomplete",
			input:    `{"a":{"b":{"c":{"d":"e"`,
			expected: `{"a":{"b":{"c":{"d":"e"}}}}`,
		},
		{
			name:     "array of incomplete objects",
			input:    `[{"a":1},{"b":2`,
			expected: `[{"a":1},{"b":2}]`,
		},
		{
			name:     "empty object incomplete",
			input:    `{`,
			expected: `{}`,
		},
		{
			name:     "empty array incomplete",
			input:    `[`,
			expected: `[]`,
		},
		{
			name:     "string with escape",
			input:    `{"text":"hello\nworld"`,
			expected: `{"text":"hello\nworld"}`,
		},
		{
			name:     "incomplete unicode escape only",
			input:    `"\u`,
			expected: `""`,
		},
		{
			name:     "incomplete unicode escape digits",
			input:    `"\u12`,
			expected: `""`,
		},
		{
			name:     "incomplete unicode escape after text",
			input:    `"text \u00`,
			expected: `"text "`,
		},
		{
			name:     "incomplete unicode escape in object",
			input:    `{"a":"\u12`,
			expected: `{"a":""}`,
		},
		{
			name:     "incomplete escape sequence",
			input:    `"value with \`,
			expected: `"value with "`,
		},
		{
			name:     "complete escape sequences",
			input:    `"value with \"quoted\" text and \\ escape`,
			expected: `"value with \"quoted\" text and \\ escape"`,
		},
		{
			name:     "multiple properties incomplete",
			input:    `{"name":"John","age":30,"city":"New`,
			expected: `{"name":"John","age":30,"city":"New"}`,
		},
		{
			name:     "scientific notation",
			input:    `{"value":1.23e-4`,
			expected: `{"value":1.23e-4}`,
		},
		{
			name:     "incomplete decimal number root",
			input:    `12.`,
			expected: `12`,
		},
		{
			name:     "incomplete negative number root",
			input:    `-`,
			expected: ``,
		},
		{
			name:     "incomplete exponent root",
			input:    `2.5e-`,
			expected: `2.5`,
		},
		{
			name:     "object key without value",
			input:    `{"key":`,
			expected: `{}`,
		},
		{
			name:     "partial second object key",
			input:    `{"k1": 1, "k2`,
			expected: `{"k1": 1}`,
		},
		{
			name:     "array trailing comma",
			input:    `[1, `,
			expected: `[1]`,
		},
		{
			name:     "nested object empty array start",
			input:    `{"a": 1, "b": [`,
			expected: `{"a": 1, "b": []}`,
		},
		{
			name:     "empty objects inside nested objects and arrays",
			input:    `{"type":"div","children":[{"type":"Card","props":{}`,
			expected: `{"type":"div","children":[{"type":"Card","props":{}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FixJSON(tt.input)
			if result != tt.expected {
				t.Errorf("FixJSON() = %q, want %q", result, tt.expected)
			}

			if result != "" {
				// Verify the result is valid JSON when a repair is possible. Some TS
				// parity cases, such as "-", intentionally truncate to empty.
				var v interface{}
				if err := json.Unmarshal([]byte(result), &v); err != nil {
					t.Errorf("FixJSON() produced invalid JSON: %v", err)
				}
			}
		})
	}
}

func TestFixJSONEmpty(t *testing.T) {
	result := FixJSON("")
	if result != "" {
		t.Errorf("FixJSON(\"\") = %q, want \"\"", result)
	}
}

func TestFixJSONComplexNested(t *testing.T) {
	input := `{"user":{"name":"Alice","profile":{"age":30,"tags":["developer","golang"`

	result := FixJSON(input)

	// Should be valid JSON
	var v interface{}
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		t.Errorf("FixJSON() produced invalid JSON: %v\nResult: %s", err, result)
	}

	// Check that structure is maintained
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}
	if _, ok := m["user"]; !ok {
		t.Error("Expected 'user' key in result")
	}
}

// Benchmark FixJSON with different input sizes
func BenchmarkFixJSON(b *testing.B) {
	inputs := []string{
		`{"name":"John"`,
		`{"user":{"name":"Alice","profile":{"age":30,"tags":["dev","go"`,
		`[{"a":1},{"b":2},{"c":3`,
	}

	for i, input := range inputs {
		b.Run(string(rune('A'+i)), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				FixJSON(input)
			}
		})
	}
}

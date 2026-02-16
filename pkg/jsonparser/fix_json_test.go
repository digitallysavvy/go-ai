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
		// Note: A trailing backslash in a string is ambiguous - we can't know what was intended
		// Skipping this edge case as it's not a realistic streaming scenario
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FixJSON(tt.input)
			if result != tt.expected {
				t.Errorf("FixJSON() = %q, want %q", result, tt.expected)
			}

			// Verify the result is valid JSON
			var v interface{}
			if err := json.Unmarshal([]byte(result), &v); err != nil {
				t.Errorf("FixJSON() produced invalid JSON: %v", err)
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

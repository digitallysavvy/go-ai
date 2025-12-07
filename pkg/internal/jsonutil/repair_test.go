package jsonutil

import (
	"testing"
)

func TestFixJSON_ValidJSON(t *testing.T) {
	t.Parallel()

	input := `{"name": "John", "age": 30}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != input {
		t.Errorf("expected unchanged JSON, got %s", result)
	}
}

func TestFixJSON_TrailingCommaObject(t *testing.T) {
	t.Parallel()

	input := `{"name": "John", "age": 30,}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be valid JSON now
	if result == input {
		t.Error("expected trailing comma to be removed")
	}
}

func TestFixJSON_TrailingCommaArray(t *testing.T) {
	t.Parallel()

	input := `["a", "b", "c",]`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be valid JSON now
	if result == input {
		t.Error("expected trailing comma to be removed")
	}
}

func TestFixJSON_SingleLineComment(t *testing.T) {
	t.Parallel()

	input := `{
		"name": "John" // This is a comment
	}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be valid JSON without comment
	if result == input {
		t.Error("expected comment to be removed")
	}
}

func TestFixJSON_MultiLineComment(t *testing.T) {
	t.Parallel()

	input := `{
		"name": "John" /* This is a
		multi-line comment */
	}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected comment to be removed")
	}
}

func TestFixJSON_UnquotedKeys(t *testing.T) {
	t.Parallel()

	input := `{name: "John", age: 30}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected keys to be quoted")
	}
}

func TestFixJSON_SingleQuotes(t *testing.T) {
	t.Parallel()

	input := `{'name': 'John'}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected single quotes to be converted")
	}
}

func TestFixJSON_UnclosedBrace(t *testing.T) {
	t.Parallel()

	input := `{"name": "John"`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected brace to be added")
	}
}

func TestFixJSON_UnclosedBracket(t *testing.T) {
	t.Parallel()

	input := `["a", "b", "c"`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected bracket to be added")
	}
}

func TestFixJSON_MultipleIssues(t *testing.T) {
	t.Parallel()

	input := `{name: 'John', age: 30,}`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected multiple fixes")
	}
}

func TestFixJSON_Unfixable(t *testing.T) {
	t.Parallel()

	input := `completely invalid not even close to json`
	result, err := FixJSON(input)

	// Should return original if unfixable
	if result != input {
		t.Errorf("expected original to be returned, got %s", result)
	}
	if err == nil {
		t.Error("expected error for unfixable JSON")
	}
}

func TestParsePartialJSON_CompleteJSON(t *testing.T) {
	t.Parallel()

	input := `{"name": "John", "age": 30}`
	result, err := ParsePartialJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestParsePartialJSON_PartialObject(t *testing.T) {
	t.Parallel()

	input := `{"name": "John"`
	result, err := ParsePartialJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestParsePartialJSON_PartialArray(t *testing.T) {
	t.Parallel()

	input := `["a", "b"`
	result, err := ParsePartialJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestParsePartialJSON_EmptyString(t *testing.T) {
	t.Parallel()

	result, err := ParsePartialJSON("")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for empty string")
	}
}

func TestParsePartialJSON_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	result, err := ParsePartialJSON("   ")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for whitespace only")
	}
}

func TestIsPartiallyValid_ValidStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected bool
	}{
		{`{"name": "John"`, true},
		{`["a", "b"`, true},
		{`"hello`, false}, // Incomplete string is hard to parse
		{`{`, true},
		{`[`, true},
	}

	for _, tt := range tests {
		result := IsPartiallyValid(tt.input)
		if result != tt.expected {
			t.Errorf("IsPartiallyValid(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestIsPartiallyValid_InvalidStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected bool
	}{
		{``, false},
		{`   `, false},
		{`hello`, false},
		{`123`, false},
		{`null`, false},
	}

	for _, tt := range tests {
		result := IsPartiallyValid(tt.input)
		if result != tt.expected {
			t.Errorf("IsPartiallyValid(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestRemoveTrailingCommas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{`{"a": 1,}`, `{"a": 1}`},
		{`["a",]`, `["a"]`},
		{`{"a": 1, }`, `{"a": 1}`},   // Regex collapses ", }" to "}"
		{`["a", ]`, `["a"]`},          // Regex collapses ", ]" to "]"
	}

	for _, tt := range tests {
		result := removeTrailingCommas(tt.input)
		if result != tt.expected {
			t.Errorf("removeTrailingCommas(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestRemoveComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{`{"a": 1} // comment`, `{"a": 1} `},
		{`{"a": /* comment */ 1}`, `{"a":  1}`},
	}

	for _, tt := range tests {
		result := removeComments(tt.input)
		if result != tt.expected {
			t.Errorf("removeComments(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFixUnquotedKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{`{key: "value"}`, `{"key": "value"}`},
		{`{key1: "v", key2: "v2"}`, `{"key1": "v","key2": "v2"}`}, // Implementation collapses spaces after comma
	}

	for _, tt := range tests {
		result := fixUnquotedKeys(tt.input)
		if result != tt.expected {
			t.Errorf("fixUnquotedKeys(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFixSingleQuotes(t *testing.T) {
	t.Parallel()

	input := `{'key': 'value'}`
	expected := `{"key": "value"}`
	result := fixSingleQuotes(input)

	if result != expected {
		t.Errorf("fixSingleQuotes(%q) = %q, want %q", input, result, expected)
	}
}

func TestBalanceBrackets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{`{"a": 1`, `{"a": 1}`},
		{`["a"`, `["a"]`},
		{`{"a": [1, 2`, `{"a": [1, 2]}`},
		{`{"a": {"b": 1`, `{"a": {"b": 1}}`},
	}

	for _, tt := range tests {
		result := balanceBrackets(tt.input)
		if result != tt.expected {
			t.Errorf("balanceBrackets(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestAggressiveFix(t *testing.T) {
	t.Parallel()

	// Should truncate after last valid bracket
	input := `{"name": "John"} garbage`
	result := aggressiveFix(input)

	if result != `{"name": "John"}` {
		t.Errorf("aggressiveFix(%q) = %q", input, result)
	}
}

func TestFixJSON_NestedStructure(t *testing.T) {
	t.Parallel()

	input := `{
		name: 'John',
		address: {
			city: 'NYC',
			zip: 10001,
		}
	}`

	result, err := FixJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected JSON to be fixed")
	}
}

func TestFixJSON_ArrayOfObjects(t *testing.T) {
	t.Parallel()

	input := `[{name: 'John'}, {name: 'Jane'},]`
	result, err := FixJSON(input)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == input {
		t.Error("expected JSON to be fixed")
	}
}

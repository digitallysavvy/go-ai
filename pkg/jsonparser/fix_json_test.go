package jsonparser

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
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

// TestFixJSONTruncatedMultiByteUTF8 is a permanent regression test for the
// jsonparser UTF-8 truncation issue flagged as "Unverified" in the bug
// report (state/parity/sep_23_2026/bug-review/R2.md): FixJSON indexes input
// byte-by-byte, so a partial JSON string that ends mid-way through a
// multi-byte UTF-8 rune (plausible for streamed non-ASCII tool-call
// arguments cut off at an arbitrary chunk boundary) used to leave the
// repaired output ending in an incomplete trailing byte sequence. That
// survived into the closed JSON string and, since encoding/json does not
// reject invalid UTF-8 in a string (it silently substitutes U+FFFD per bad
// byte instead of erroring), decoded as mangled replacement characters
// instead of simply omitting the not-yet-fully-streamed character.
func TestFixJSONTruncatedMultiByteUTF8(t *testing.T) {
	full := `{"a":"` + "日本語" + `"}`

	// Truncate the input in the middle of "日" (E6 97 A5 in UTF-8): keep
	// only its first two bytes.
	cut := len(`{"a":"`) + 2
	partial := full[:cut]
	if utf8.ValidString(partial) {
		t.Fatalf("test setup: partial input %q unexpectedly already valid UTF-8", partial)
	}

	result := FixJSON(partial)
	if !utf8.ValidString(result) {
		t.Fatalf("FixJSON(%q) = %q, which is not valid UTF-8", partial, result)
	}

	var v map[string]interface{}
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		t.Fatalf("FixJSON() produced invalid JSON: %v\nResult: %q", err, result)
	}
	// The incomplete trailing rune must be dropped entirely, not replaced
	// with a mangled U+FFFD.
	if got := v["a"]; got != "" {
		t.Fatalf(`v["a"] = %q, want "" (the incomplete trailing character dropped cleanly)`, got)
	}
}

// TestFixJSONPreservesCompleteMultiByteUTF8 guards against an overzealous
// fix: a string ending in a *complete* multi-byte rune must survive intact.
func TestFixJSONPreservesCompleteMultiByteUTF8(t *testing.T) {
	partial := `{"a":"` + "日本語"
	result := FixJSON(partial)
	if !utf8.ValidString(result) {
		t.Fatalf("FixJSON(%q) = %q, which is not valid UTF-8", partial, result)
	}
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		t.Fatalf("FixJSON() produced invalid JSON: %v\nResult: %q", err, result)
	}
	if got := v["a"]; got != "日本語" {
		t.Fatalf(`v["a"] = %q, want "日本語"`, got)
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

// TestTrimIncompleteUTF8SuffixOnlyTrimsFinalRune checks that an invalid
// byte earlier in the string doesn't cause trailing valid bytes to be
// dropped; only an incomplete final rune is trimmed.
func TestTrimIncompleteUTF8SuffixOnlyTrimsFinalRune(t *testing.T) {
	cases := map[string]string{
		"abc":             "abc",
		"ab\xe2\x82":      "ab",       // "€" cut after two bytes
		"ab\xf0\x9f\x98":  "ab",       // 4-byte emoji cut after three
		"a\xffbcd":        "a\xffbcd", // invalid byte in the middle: keep the tail
		"a\xffbc\xe2\x82": "a\xffbc",  // invalid middle byte plus a cut final rune
		"\xe2\x82\xac":    "\xe2\x82\xac",
		"":                "",
	}
	for in, want := range cases {
		if got := trimIncompleteUTF8Suffix(in); got != want {
			t.Errorf("trimIncompleteUTF8Suffix(%q) = %q, want %q", in, got, want)
		}
	}
}

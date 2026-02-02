package jsonparser

import (
	"testing"
)

func TestParsePartialJSON(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedState ParseState
		shouldHaveValue bool
	}{
		{
			name:          "empty string",
			input:         "",
			expectedState: ParseStateUndefinedInput,
			shouldHaveValue: false,
		},
		{
			name:          "valid complete object",
			input:         `{"name":"John"}`,
			expectedState: ParseStateSuccessful,
			shouldHaveValue: true,
		},
		{
			name:          "valid complete array",
			input:         `[1,2,3]`,
			expectedState: ParseStateSuccessful,
			shouldHaveValue: true,
		},
		{
			name:          "incomplete object - repaired",
			input:         `{"name":"John"`,
			expectedState: ParseStateRepaired,
			shouldHaveValue: true,
		},
		{
			name:          "incomplete array - repaired",
			input:         `[1,2,3`,
			expectedState: ParseStateRepaired,
			shouldHaveValue: true,
		},
		{
			name:          "incomplete nested - repaired",
			input:         `{"user":{"name":"Alice"`,
			expectedState: ParseStateRepaired,
			shouldHaveValue: true,
		},
		{
			name:          "incomplete literal - repaired",
			input:         `{"active":tr`,
			expectedState: ParseStateRepaired,
			shouldHaveValue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePartialJSON(tt.input)

			if result.State != tt.expectedState {
				t.Errorf("ParsePartialJSON().State = %v, want %v", result.State, tt.expectedState)
			}

			if tt.shouldHaveValue && result.Value == nil {
				t.Error("Expected non-nil value")
			}

			if !tt.shouldHaveValue && result.Value != nil {
				t.Error("Expected nil value")
			}

			// Check that successful and repaired states have no error
			if (result.State == ParseStateSuccessful || result.State == ParseStateRepaired) && result.Error != nil {
				t.Errorf("Expected no error for state %v, got %v", result.State, result.Error)
			}
		})
	}
}

func TestParsePartialJSONValues(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, value interface{})
	}{
		{
			name:  "parse object",
			input: `{"name":"John","age":30}`,
			validate: func(t *testing.T, value interface{}) {
				m, ok := value.(map[string]interface{})
				if !ok {
					t.Fatal("Expected map")
				}
				if m["name"] != "John" {
					t.Errorf("Expected name=John, got %v", m["name"])
				}
				if m["age"].(float64) != 30 {
					t.Errorf("Expected age=30, got %v", m["age"])
				}
			},
		},
		{
			name:  "parse array",
			input: `[1,2,3]`,
			validate: func(t *testing.T, value interface{}) {
				arr, ok := value.([]interface{})
				if !ok {
					t.Fatal("Expected array")
				}
				if len(arr) != 3 {
					t.Errorf("Expected array length 3, got %d", len(arr))
				}
			},
		},
		{
			name:  "parse incomplete object",
			input: `{"items":[1,2,3`,
			validate: func(t *testing.T, value interface{}) {
				m, ok := value.(map[string]interface{})
				if !ok {
					t.Fatal("Expected map")
				}
				items, ok := m["items"].([]interface{})
				if !ok {
					t.Fatal("Expected items array")
				}
				if len(items) != 3 {
					t.Errorf("Expected items length 3, got %d", len(items))
				}
			},
		},
		{
			name:  "parse boolean true",
			input: `{"active":true}`,
			validate: func(t *testing.T, value interface{}) {
				m := value.(map[string]interface{})
				if m["active"] != true {
					t.Error("Expected active=true")
				}
			},
		},
		{
			name:  "parse incomplete boolean",
			input: `{"active":fals`,
			validate: func(t *testing.T, value interface{}) {
				m := value.(map[string]interface{})
				if m["active"] != false {
					t.Error("Expected active=false after repair")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePartialJSON(tt.input)

			if result.Value == nil {
				t.Fatal("Expected non-nil value")
			}

			tt.validate(t, result.Value)
		})
	}
}

func TestParsePartialJSONStreamingScenario(t *testing.T) {
	// Simulate streaming scenario where JSON is built up progressively
	chunks := []struct{
		json string
		shouldParse bool
	}{
		{`{`, true},
		{`{"name":`, false}, // Truly incomplete - no value started
		{`{"name":"Alice"`, true},
		{`{"name":"Alice","age":`, false}, // Truly incomplete - no value started
		{`{"name":"Alice","age":30`, true},
		{`{"name":"Alice","age":30}`, true},
	}

	for i, chunk := range chunks {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			result := ParsePartialJSON(chunk.json)

			if chunk.shouldParse {
				// Should successfully parse (either directly or after repair)
				if result.State == ParseStateFailed {
					t.Errorf("Expected successful/repaired state for chunk %d, got %v", i, result.State)
				}
				if result.Value == nil {
					t.Errorf("Expected non-nil value for chunk %d", i)
				}
			} else {
				// These truly incomplete JSONs may not parse, which is acceptable
				// They will parse once more data arrives
				t.Logf("Chunk %d (%s) parse state: %v (acceptable)", i, chunk.json, result.State)
			}
		})
	}
}

// Benchmark ParsePartialJSON
func BenchmarkParsePartialJSON(b *testing.B) {
	inputs := []struct {
		name  string
		input string
	}{
		{"complete", `{"name":"John","age":30}`},
		{"incomplete", `{"name":"John","age":30`},
		{"large_complete", `{"users":[{"name":"Alice","age":30},{"name":"Bob","age":25}]}`},
		{"large_incomplete", `{"users":[{"name":"Alice","age":30},{"name":"Bob","age":25`},
	}

	for _, input := range inputs {
		b.Run(input.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				ParsePartialJSON(input.input)
			}
		})
	}
}

package openai

import (
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestNormalizeOpenAIJSONSchemaRemovesPropertyNames ports
// normalize-openai-json-schema.test.ts's "removes string propertyNames
// recursively and warns" (d5e3024).
func TestNormalizeOpenAIJSONSchemaRemovesPropertyNames(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"variables": map[string]interface{}{
				"type": "object",
				"propertyNames": map[string]interface{}{
					"type":   "string",
					"format": "uuid",
				},
				"additionalProperties": map[string]interface{}{
					"type": "object",
					"propertyNames": map[string]interface{}{
						"type":    "string",
						"pattern": "^[A-Z_]+$",
					},
				},
			},
		},
		"definitions": map[string]interface{}{
			"variable": map[string]interface{}{
				"type": "object",
				"propertyNames": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"$defs": map[string]interface{}{
			"conditional": map[string]interface{}{
				"if": map[string]interface{}{
					"type": "object",
					"propertyNames": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}

	got, warnings, err := NormalizeOpenAIJSONSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"variables": map[string]interface{}{
				"type": "object",
				"additionalProperties": map[string]interface{}{
					"type": "object",
				},
			},
		},
		"definitions": map[string]interface{}{
			"variable": map[string]interface{}{
				"type": "object",
			},
		},
		"$defs": map[string]interface{}{
			"conditional": map[string]interface{}{
				"if": map[string]interface{}{
					"type": "object",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema = %#v, want %#v", got, want)
	}
	if len(warnings) != 1 || warnings[0].Type != "compatibility" || warnings[0].Feature != "JSON Schema propertyNames" {
		t.Fatalf("warnings = %#v", warnings)
	}

	// The input schema must not be mutated (schema.properties?.variables
	// still has propertyNames in TS).
	variables := schema["properties"].(map[string]interface{})["variables"].(map[string]interface{})
	if _, ok := variables["propertyNames"]; !ok {
		t.Fatalf("input schema was mutated: propertyNames removed from caller's copy")
	}
}

// TestNormalizeOpenAIJSONSchemaRejectsNonStringPropertyNames ports "rejects
// non-string propertyNames schemas".
func TestNormalizeOpenAIJSONSchemaRejectsNonStringPropertyNames(t *testing.T) {
	_, _, err := NormalizeOpenAIJSONSchema(map[string]interface{}{
		"type":          "object",
		"propertyNames": map[string]interface{}{"type": "number"},
	})
	var target *providererrors.UnsupportedFunctionalityError
	if err == nil {
		t.Fatal("expected UnsupportedFunctionalityError, got nil")
	}
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %T %v, want %T", err, err, target)
	}
}

// TestNormalizeOpenAIJSONSchemaRejectsBooleanPropertyNames covers the boolean
// propertyNames variant of the same TS check
// (`typeof propertyNames === 'boolean'`).
func TestNormalizeOpenAIJSONSchemaRejectsBooleanPropertyNames(t *testing.T) {
	_, _, err := NormalizeOpenAIJSONSchema(map[string]interface{}{
		"type":          "object",
		"propertyNames": true,
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

// TestNormalizeOpenAIJSONSchemaRemovesLookaroundPatterns ports "removes
// regex lookaround patterns recursively and warns" (411b3f2).
func TestNormalizeOpenAIJSONSchemaRemovesLookaroundPatterns(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"email": map[string]interface{}{
				"type":    "string",
				"format":  "email",
				"pattern": `^(?!\.)(?!.*\.\.).+@.+$`,
			},
			"username": map[string]interface{}{
				"type":    "string",
				"pattern": "^@[a-zA-Z0-9_]+$",
			},
			"contacts": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"value": map[string]interface{}{
							"type":    "string",
							"pattern": "(?<=prefix)value",
						},
					},
				},
			},
		},
		"$defs": map[string]interface{}{
			"value": map[string]interface{}{
				"type":    "string",
				"pattern": "value(?=suffix)",
			},
		},
	}

	got, warnings, err := NormalizeOpenAIJSONSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"email": map[string]interface{}{
				"type":   "string",
				"format": "email",
			},
			"username": map[string]interface{}{
				"type":    "string",
				"pattern": "^@[a-zA-Z0-9_]+$",
			},
			"contacts": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"value": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
		"$defs": map[string]interface{}{
			"value": map[string]interface{}{
				"type": "string",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema = %#v, want %#v", got, want)
	}
	if len(warnings) != 1 || warnings[0].Feature != "JSON Schema pattern with regex lookaround" {
		t.Fatalf("warnings = %#v", warnings)
	}

	email := schema["properties"].(map[string]interface{})["email"].(map[string]interface{})
	if _, ok := email["pattern"]; !ok {
		t.Fatalf("input schema was mutated: pattern removed from caller's copy")
	}
}

// TestNormalizeOpenAIJSONSchemaPreservesEscapedLookaroundLikeText ports
// "preserves escaped and character-class lookaround-like text".
func TestNormalizeOpenAIJSONSchemaPreservesEscapedLookaroundLikeText(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"escaped": map[string]interface{}{
				"type":    "string",
				"pattern": `\(\?=literal\)`,
			},
			"characterClass": map[string]interface{}{
				"type":    "string",
				"pattern": "[(?=!)]",
			},
		},
	}

	got, warnings, err := NormalizeOpenAIJSONSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, schema) {
		t.Fatalf("schema = %#v, want unchanged %#v", got, schema)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
}

func TestContainsRegexLookaroundTable(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"^(?!\\.)(?!.*\\.\\.).+@.+$", true},
		{"(?<=prefix)value", true},
		{"value(?=suffix)", true},
		{"(?<!neg)value", true},
		{"^@[a-zA-Z0-9_]+$", false},
		{`\(\?=literal\)`, false},
		{"[(?=!)]", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			if got := containsRegexLookaround(tt.pattern); got != tt.want {
				t.Errorf("containsRegexLookaround(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

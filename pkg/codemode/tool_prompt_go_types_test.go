package codemode

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Schemas built in Go carry []string / int values; the prompt must read them
// the way it reads the serialized schema TS sees.
func TestToolDescriptionReadsGoTypedSchemas(t *testing.T) {
	tools := ToolSet{"weather": types.Tool{Name: "weather", Description: "d", Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"city":  map[string]interface{}{"type": "string", "enum": []string{"a", "b"}},
			"n":     map[string]interface{}{"type": []string{"string", "null"}},
			"count": map[string]interface{}{"enum": []int{1, 2}},
			"z":     map[string]interface{}{"const": nil},
			"html":  map[string]interface{}{"type": "string", "default": "<b>&"},
		},
		"required": []string{"city"},
	}}}
	got := BuildCodeModeToolDescription(tools, ToolDiscoveryDescription)
	for _, want := range []string{
		`city: "a" | "b";`,
		`n?: string | null;`,
		`count?: 1 | 2;`,
		`z?: null;`,
		`html?: string;`,
		`"html":"<b>&"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("description lacks %q:\n%s", want, got)
		}
	}
}

// A null default or const is still a value (TS checks !== undefined).
func TestSampleKeepsNullDefault(t *testing.T) {
	got := sampleFromSchema(jsonSchemaMap{"default": nil, "type": "string"}, jsonSchemaMap{})
	if got != nil {
		t.Fatalf("sample = %v, want nil", got)
	}
}

// type "array" with properties renders as an object in TS (properties win).
func TestObjectPropertiesBeatArrayType(t *testing.T) {
	got := schemaToType(jsonSchemaMap{"type": "array", "properties": jsonSchemaMap{"a": jsonSchemaMap{"type": "string"}}}, jsonSchemaMap{})
	if got != "{ a?: string; }" {
		t.Fatalf("type = %q", got)
	}
}

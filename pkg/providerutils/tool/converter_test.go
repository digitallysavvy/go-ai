package tool

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BUG-T15: strict mode must be included in the tool function definition so that
// Bedrock and Groq (which both call ToJSONSchema) forward it to the provider (#12893).
func TestToJSONSchema_StrictModeIncluded(t *testing.T) {
	tool := types.Tool{
		Name:        "my_tool",
		Description: "does something",
		Strict:      types.BoolPtr(true),
	}

	schema := ToJSONSchema(tool)

	fn, ok := schema["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'function' key with map value, got %T", schema["function"])
	}

	strictVal, ok := fn["strict"]
	if !ok {
		t.Errorf("expected 'strict' key in function definition when Strict=true")
	} else if strictVal != true {
		t.Errorf("strict = %v, want true", strictVal)
	}
}

// TestToJSONSchema_StrictModeExplicitFalseIsForwarded verifies that an
// explicit Strict=false IS forwarded to the wire (not dropped), matching the
// TS OpenAI-compatible-family prepareTools pattern `tool.strict != null ?
// {strict: tool.strict} : {}`: the nil-vs-false distinction Tool.Strict
// (*bool) exists to preserve must reach the wire, not just the true case.
func TestToJSONSchema_StrictModeExplicitFalseIsForwarded(t *testing.T) {
	tool := types.Tool{
		Name:        "my_tool",
		Description: "does something",
		Strict:      types.BoolPtr(false),
	}

	schema := ToJSONSchema(tool)

	fn, ok := schema["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'function' key with map value, got %T", schema["function"])
	}

	strictVal, ok := fn["strict"]
	if !ok {
		t.Fatal("expected 'strict' key in function definition when Strict=false (explicit, not unset)")
	}
	if strictVal != false {
		t.Errorf("strict = %v, want false", strictVal)
	}
}

// TestToJSONSchema_StrictModeOmittedWhenUnset verifies that strict is
// omitted entirely (not defaulted to false) when Tool.Strict was never set.
func TestToJSONSchema_StrictModeOmittedWhenUnset(t *testing.T) {
	tool := types.Tool{Name: "my_tool", Description: "does something"}

	schema := ToJSONSchema(tool)

	fn, ok := schema["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'function' key with map value, got %T", schema["function"])
	}

	if _, ok := fn["strict"]; ok {
		t.Errorf("strict should not be present in function definition when Strict is unset")
	}
}

func TestToAnthropicFormatSanitizesUnsupportedValidationKeywords(t *testing.T) {
	tools := []types.Tool{{
		Name:        "search",
		Description: "search",
		Parameters: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"q": map[string]interface{}{
					"type":      "string",
					"minLength": 1,
					"pattern":   "^[a-z]+$",
				},
			},
			"required": []interface{}{"q"},
		},
	}}

	result := ToAnthropicFormat(tools)
	schema := result[0]["input_schema"].(map[string]interface{})
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", schema["additionalProperties"])
	}
	props := schema["properties"].(map[string]interface{})
	q := props["q"].(map[string]interface{})
	if _, ok := q["minLength"]; ok {
		t.Fatal("nested minLength should be removed")
	}
	if _, ok := q["pattern"]; ok {
		t.Fatal("nested pattern should be removed")
	}
	if q["description"] != "min length: 1; pattern: ^[a-z]+$." {
		t.Fatalf("description = %q, want constraints moved into description", q["description"])
	}
	if q["type"] != "string" {
		t.Fatalf("type should be preserved, got %v", q["type"])
	}
}

// TestToOpenAIFormat_StrictModeForwarded verifies the full ToOpenAIFormat path
// (used by OpenAI, Groq, DeepSeek, Mistral, Alibaba, and other
// OpenAI-compatible providers) forwards strict mode whenever it was
// explicitly set (true OR false), and omits it entirely when unset.
func TestToOpenAIFormat_StrictModeForwarded(t *testing.T) {
	tools := []types.Tool{
		{Name: "strict_tool", Description: "strict", Strict: types.BoolPtr(true)},
		{Name: "non_strict_tool", Description: "non-strict", Strict: types.BoolPtr(false)},
		{Name: "unspecified_tool", Description: "unspecified"},
	}

	formatted := ToOpenAIFormat(tools)

	if len(formatted) != 3 {
		t.Fatalf("expected 3 formatted tools, got %d", len(formatted))
	}

	// First tool must have strict=true.
	fn0, _ := formatted[0]["function"].(map[string]interface{})
	if fn0["strict"] != true {
		t.Errorf("formatted[0] strict = %v, want true", fn0["strict"])
	}

	// Second tool has an EXPLICIT strict=false, which must be forwarded.
	fn1, _ := formatted[1]["function"].(map[string]interface{})
	if strictVal, ok := fn1["strict"]; !ok || strictVal != false {
		t.Errorf("formatted[1] strict = %v (ok=%v), want false", strictVal, ok)
	}

	// Third tool never set Strict, so the field must be omitted entirely.
	fn2, _ := formatted[2]["function"].(map[string]interface{})
	if _, ok := fn2["strict"]; ok {
		t.Errorf("formatted[2] strict should be omitted when unset")
	}
}

func TestToJSONSchema_DefaultParametersIncludeObjectType(t *testing.T) {
	schema := ToJSONSchema(types.Tool{Name: "lookup"})
	fn := schema["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})
	if params["type"] != "object" {
		t.Fatalf("parameters.type = %v, want object", params["type"])
	}
	if _, ok := params["properties"].(map[string]interface{}); !ok {
		t.Fatalf("parameters.properties = %#v", params["properties"])
	}
}

func TestToJSONSchema_ImplicitObjectSchemaGetsType(t *testing.T) {
	schema := ToJSONSchema(types.Tool{
		Name: "lookup",
		Parameters: map[string]interface{}{
			"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
		},
	})
	fn := schema["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})
	if params["type"] != "object" {
		t.Fatalf("parameters.type = %v, want object", params["type"])
	}
}

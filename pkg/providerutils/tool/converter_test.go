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
		Strict:      true,
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

// TestToJSONSchema_StrictModeOmittedWhenFalse verifies that strict is not included
// when the tool's Strict field is false (avoids sending unnecessary fields).
func TestToJSONSchema_StrictModeOmittedWhenFalse(t *testing.T) {
	tool := types.Tool{
		Name:        "my_tool",
		Description: "does something",
		Strict:      false,
	}

	schema := ToJSONSchema(tool)

	fn, ok := schema["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'function' key with map value, got %T", schema["function"])
	}

	if _, ok := fn["strict"]; ok {
		t.Errorf("strict should not be present in function definition when Strict=false")
	}
}

// TestToOpenAIFormat_StrictModeForwarded verifies the full ToOpenAIFormat path
// (used by OpenAI, Groq, and Bedrock providers) also forwards strict mode.
func TestToOpenAIFormat_StrictModeForwarded(t *testing.T) {
	tools := []types.Tool{
		{Name: "strict_tool", Description: "strict", Strict: true},
		{Name: "normal_tool", Description: "normal", Strict: false},
	}

	formatted := ToOpenAIFormat(tools)

	if len(formatted) != 2 {
		t.Fatalf("expected 2 formatted tools, got %d", len(formatted))
	}

	// First tool must have strict=true.
	fn0, _ := formatted[0]["function"].(map[string]interface{})
	if fn0["strict"] != true {
		t.Errorf("formatted[0] strict = %v, want true", fn0["strict"])
	}

	// Second tool must not have strict.
	fn1, _ := formatted[1]["function"].(map[string]interface{})
	if _, ok := fn1["strict"]; ok {
		t.Errorf("formatted[1] strict should not be set when Strict=false")
	}
}

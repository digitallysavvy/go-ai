package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestSerializableSchemaRoundTrip(t *testing.T) {
	in := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"city":  map[string]interface{}{"type": "string", "description": "City name"},
			"count": map[string]interface{}{"type": "integer", "enum": []interface{}{1, 2, 3}},
		},
		"required": []interface{}{"city"},
	}
	m, err := MarshalSchema(in)
	if err != nil {
		t.Fatalf("MarshalSchema() error = %v", err)
	}
	out, err := UnmarshalSchema(m)
	if err != nil {
		t.Fatalf("UnmarshalSchema() error = %v", err)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	if obj["type"] != "object" {
		t.Fatalf("unexpected type: %v", obj["type"])
	}
}

func TestSerializeToolSetRoundTripOmitsFunctions(t *testing.T) {
	tools := []types.Tool{{
		Name:        "weather",
		Description: "Get weather",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"city"},
		},
		Strict:           true,
		ProviderExecuted: true,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "hidden", nil
		},
	}}
	serialized, err := SerializeToolSet(tools)
	if err != nil {
		t.Fatalf("SerializeToolSet() error = %v", err)
	}
	def, ok := serialized["weather"]
	if !ok {
		t.Fatal("missing serialized tool")
	}
	if def.Parameters["type"] != "object" || !def.Strict || !def.ProviderExecuted {
		t.Fatalf("unexpected serialized def: %+v", def)
	}
	resolved := ResolveSerializableTools(serialized)
	if len(resolved) != 1 {
		t.Fatalf("expected one resolved tool, got %d", len(resolved))
	}
	if resolved[0].Execute != nil {
		t.Fatal("serialized tool should not restore function fields")
	}
	if resolved[0].Name != "weather" || !resolved[0].Strict || !resolved[0].ProviderExecuted {
		t.Fatalf("unexpected resolved tool: %+v", resolved[0])
	}
	if err := ValidateSerializableToolInput(def, map[string]interface{}{"city": "Tokyo"}); err != nil {
		t.Fatalf("expected valid input: %v", err)
	}
	if err := ValidateSerializableToolInput(def, map[string]interface{}{"city": 123}); err == nil {
		t.Fatal("expected validation error")
	}
}

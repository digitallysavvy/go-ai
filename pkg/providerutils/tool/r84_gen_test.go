package tool

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// CR r84 provb -> gen: a bare schema.Validator Parameters value resolves to
// its JSON Schema map (TS asSchema(inputSchema).jsonSchema is a plain object).
func TestR84genResolveToolSchemasBareValidator(t *testing.T) {
	m := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}
	out := ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: schema.NewJSONSchema(m)}})
	got, ok := out[0].Parameters.(map[string]interface{})
	if !ok || got["type"] != "object" {
		t.Fatalf("Parameters = %#v, want the JSON Schema map", out[0].Parameters)
	}

	var nilValidator *schema.JSONSchemaValidator
	out = ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: nilValidator}})
	if out[0].Parameters != nil {
		t.Fatalf("typed nil validator: Parameters = %#v, want nil", out[0].Parameters)
	}
	out = ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: schema.NewJSONSchema(nil)}})
	if out[0].Parameters != nil {
		t.Fatalf("nil schema validator: Parameters = %#v, want nil", out[0].Parameters)
	}
}

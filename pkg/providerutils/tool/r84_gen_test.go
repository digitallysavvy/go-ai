package tool

import (
	"encoding/json"
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

	// A typed nil or nil-schema validator is no schema: a function tool then
	// gets the TS asSchema(undefined) default (CR 2), a provider tool none.
	var nilValidator *schema.JSONSchemaValidator
	for _, p := range []interface{}{nilValidator, schema.NewJSONSchema(nil)} {
		out = ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: p}, {Name: "p", Type: types.ToolTypeProviderDefined, Parameters: p}})
		if b, _ := json.Marshal(out[0].Parameters); string(b) != r84genEmptyObjectSchema {
			t.Fatalf("%#v: Parameters = %s", p, b)
		}
		if out[1].Parameters != nil {
			t.Fatalf("%#v: provider tool Parameters = %#v, want nil", p, out[1].Parameters)
		}
	}
}

const r84genEmptyObjectSchema = `{"type":"object","properties":{},"additionalProperties":false}`

// CR r84 provb -> gen 2: TS prepareTools sends
// asSchema(tool.inputSchema).jsonSchema, and asSchema(undefined) is
// { type: 'object', properties: {}, additionalProperties: false }.
func TestR84genToolWithoutParametersGetsTSDefaultSchema(t *testing.T) {
	tools := []types.Tool{
		{Name: "fn"},
		{Name: "dyn", Type: types.ToolTypeDynamic},
		{Name: "provider", Type: types.ToolTypeProviderDefined, ProviderID: "google.google_search"},
	}
	out := ResolveToolSchemas(tools)
	for _, i := range []int{0, 1} {
		if b, _ := json.Marshal(out[i].Parameters); string(b) != r84genEmptyObjectSchema {
			t.Fatalf("%s Parameters = %s", out[i].Name, b)
		}
	}
	if out[2].Parameters != nil || tools[0].Parameters != nil {
		t.Fatalf("provider tool = %#v, input modified = %v", out[2].Parameters, tools[0].Parameters != nil)
	}
	// The Chat converter keeps the ordered default.
	b, _ := json.Marshal(ToJSONSchema(out[0]))
	if want := `{"function":{"name":"fn","parameters":` + r84genEmptyObjectSchema + `},"type":"function"}`; string(b) != want {
		t.Fatalf("wire = %s, want %s", b, want)
	}
}

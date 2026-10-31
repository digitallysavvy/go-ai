package tool

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func TestResolveToolSchemasReplacesSchemaParameters(t *testing.T) {
	m := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}
	tools := []types.Tool{
		{Name: "schema", Parameters: schema.NewSimpleJSONSchema(m)},
		{Name: "map", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "none"},
	}
	got := ResolveToolSchemas(tools)
	if _, ok := got[0].Parameters.(map[string]interface{}); !ok {
		t.Fatalf("schema tool Parameters = %T, want map", got[0].Parameters)
	}
	if _, ok := tools[0].Parameters.(*schema.SimpleJSONSchema); !ok {
		t.Fatal("input tools were modified")
	}
	b, _ := json.Marshal(ToJSONSchema(got[0]))
	if want := `{"function":{"name":"schema","parameters":{"properties":{"a":{"type":"string"}},"type":"object"}},"type":"function"}`; string(b) != want {
		t.Fatalf("wire = %s, want %s", b, want)
	}
	if &ResolveToolSchemas(tools[1:])[0] != &tools[1] {
		t.Fatal("slice without schemas should be returned unchanged")
	}
}

func TestToJSONSchemaKeepsOrderedSchemaOrder(t *testing.T) {
	o, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: o}})
	b, _ := json.Marshal(ToJSONSchema(got[0]))
	want := `{"function":{"name":"t","parameters":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"string"}}}},"type":"function"}`
	if string(b) != want {
		t.Fatalf("wire = %s, want %s", b, want)
	}
}

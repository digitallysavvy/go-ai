package moonshot

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type r84provbArgs struct {
	City string `json:"city"`
}

// TS normalizeJsonSchemaForMFJS(tool.inputSchema) gets the JSON Schema of
// every tool. A schema.Schema Parameters value (JSON or struct schema) failed
// the MFJS root check ("must be a JSON Schema object with type object")
// because it was not read as a schema.
func TestR84provbSchemaValueParametersNormalized(t *testing.T) {
	cases := []struct {
		params interface{}
		want   string
	}{
		{
			schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}}),
			`{"properties":{"city":{"type":"string"}},"type":"object"}`,
		},
		{
			schema.NewSimpleStructSchema(reflect.TypeOf(r84provbArgs{})),
			`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"city":{"type":"string"}},"additionalProperties":false}`,
		},
	}
	for _, c := range cases {
		tools, _, _, err := prepareMoonshotTools([]types.Tool{{Name: "t", Parameters: c.params}}, types.ToolChoice{}, false, "kimi-k2-0905-preview")
		if err != nil {
			t.Fatalf("%T: %v", c.params, err)
		}
		b, err := json.Marshal(tools[0].Function.Parameters)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Fatalf("%T: parameters =\n%s\nwant\n%s", c.params, b, c.want)
		}
	}
}

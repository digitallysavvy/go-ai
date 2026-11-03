package tool

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A nil schema value (typed nil pointer, zero SimpleJSONSchema whose
// validator is nil) is no schema, TS asSchema(undefined): tool conversion
// sends the default object parameters instead of calling into the nil value
// (gen CR r82 item 1).
func TestR82integNilSchemaParametersDefaultObject(t *testing.T) {
	for name, p := range map[string]interface{}{
		"typed nil SimpleJSONSchema":   (*schema.SimpleJSONSchema)(nil),
		"zero SimpleJSONSchema":        &schema.SimpleJSONSchema{},
		"typed nil SimpleStructSchema": (*schema.SimpleStructSchema)(nil),
		"zero SimpleStructSchema":      &schema.SimpleStructSchema{},
		"nil-map SimpleJSONSchema":     schema.NewSimpleJSONSchema(nil),
	} {
		t.Run(name, func(t *testing.T) {
			got := ToJSONSchema(types.Tool{Name: "t", Parameters: p})
			b, _ := json.Marshal(got["function"].(map[string]interface{})["parameters"])
			if string(b) != `{"properties":{},"type":"object"}` {
				t.Fatalf("parameters = %s", b)
			}
			// r84: a function tool without a schema gets TS asSchema(undefined).
			resolved := ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: p}})
			if b, _ := json.Marshal(resolved[0].Parameters); string(b) != `{"type":"object","properties":{},"additionalProperties":false}` {
				t.Fatalf("ResolveToolSchemas Parameters = %s", b)
			}
			if s := SanitizeAnthropicSchema(p); s != nil {
				t.Fatalf("SanitizeAnthropicSchema = %#v, want nil", s)
			}
		})
	}
}

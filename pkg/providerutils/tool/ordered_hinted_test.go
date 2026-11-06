package tool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func TestResolveToolSchemasTurnsHintedIntoOrderedSchema(t *testing.T) {
	raw := []byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`)
	hint, _ := jsonorder.Parse(raw)
	var v interface{}
	_ = json.Unmarshal(raw, &v)
	tools := ResolveToolSchemas([]types.Tool{{Name: "t", Parameters: jsonorder.Hinted{V: v, Hint: hint}}})
	o, ok := tools[0].Parameters.(*schema.OrderedSchema)
	if !ok {
		t.Fatalf("Parameters = %T, want *schema.OrderedSchema", tools[0].Parameters)
	}
	b, _ := o.MarshalJSON()
	if strings.Index(string(b), "zeta") > strings.Index(string(b), "alpha") {
		t.Fatalf("order lost: %s", b)
	}
}

package tool

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Integration of r6-core (OrderedSchema, schema.Schema resolution) and
// r6-prov-anthgoog (raw-schema ordering for Anthropic): an OrderedSchema keeps
// its source key order in both converters, and a SimpleJSONSchema still
// resolves to its JSON Schema instead of {}.
func TestToolSchemasKeepOrderAndResolveAcrossConverters(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"zeta":{"type":"string","minLength":2},"alpha":{"type":"integer"}},"required":["zeta"]}`))
	if err != nil {
		t.Fatal(err)
	}
	fn, err := jsonstr.Marshal(ToJSONSchema(types.Tool{Name: "t", Parameters: ordered}))
	if err != nil {
		t.Fatal(err)
	}
	if i, j := strings.Index(string(fn), `"zeta"`), strings.Index(string(fn), `"alpha"`); i < 0 || j < 0 || i > j {
		t.Fatalf("function tool lost the ordered schema key order: %s", fn)
	}

	anth, err := jsonstr.Marshal(SanitizeAnthropicSchema(ordered))
	if err != nil {
		t.Fatal(err)
	}
	if i, j := strings.Index(string(anth), `"zeta"`), strings.Index(string(anth), `"alpha"`); i < 0 || j < 0 || i > j {
		t.Fatalf("anthropic schema lost the ordered schema key order: %s", anth)
	}
	if !strings.Contains(string(anth), `"additionalProperties":false`) {
		t.Fatalf("anthropic schema not sanitized: %s", anth)
	}

	simple := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}})
	out, err := jsonstr.Marshal(ToJSONSchema(types.Tool{Name: "s", Parameters: simple}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"q":{"type":"string"}`) {
		t.Fatalf("simple JSON schema not resolved: %s", out)
	}
}

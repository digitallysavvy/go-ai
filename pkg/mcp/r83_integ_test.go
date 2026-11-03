package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	toolutil "github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type r83integWeatherInput struct {
	Zip   string `json:"zip" validate:"required"`
	City  string `json:"city,omitempty"`
	Email string `json:"email,omitempty" validate:"omitempty,email"`
}

// TestR83integExplicitInputSchemaKeptAsGiven: TS toolsFromDefinitions uses
// schemas[name].inputSchema as given (mcp-client.ts 1352), so a struct
// schema keeps its field order on the wire and its own validator checks the
// tool input; a typed nil or zero schema value is no schema and does not
// panic.
func TestR83integExplicitInputSchemaKeptAsGiven(t *testing.T) {
	converter := NewMCPToolConverter(NewMCPClient(newMockTransport(), MCPClientConfig{}))
	defs := []MCPTool{{Name: "weather", InputSchema: map[string]interface{}{"type": "object"}}}
	st := schema.NewSimpleStructSchema(reflect.TypeOf(r83integWeatherInput{}))
	tools, err := converter.ToolsFromDefinitions(defs, map[string]MCPToolSchema{"weather": {InputSchema: st}})
	if err != nil {
		t.Fatal(err)
	}
	if tools[0].Parameters != interface{}(st) {
		t.Fatalf("Parameters = %T, want the given struct schema", tools[0].Parameters)
	}
	b, _ := json.Marshal(toolutil.ResolveToolSchemas(tools)[0].Parameters)
	if zi, ci := strings.Index(string(b), `"zip"`), strings.Index(string(b), `"city"`); zi < 0 || ci < zi {
		t.Fatalf("wire schema not in field order: %s", b)
	}
	if err := tools[0].Parameters.(schema.Schema).Validator().Validate(map[string]interface{}{"zip": "1", "email": "nope"}); err == nil {
		t.Fatal("struct validator did not reject an invalid email")
	}

	for name, in := range map[string]interface{}{
		"zero simple":      &schema.SimpleJSONSchema{},
		"typed nil simple": (*schema.SimpleJSONSchema)(nil),
		"typed nil struct": (*schema.SimpleStructSchema)(nil),
	} {
		tools, err := converter.ToolsFromDefinitions(defs, map[string]MCPToolSchema{"weather": {InputSchema: in}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tools[0].Parameters != nil {
			t.Fatalf("%s: Parameters = %#v, want nil", name, tools[0].Parameters)
		}
	}

	// A map keeps the JSON Schema map path.
	tools, err = converter.ToolsFromDefinitions(defs, map[string]MCPToolSchema{"weather": {InputSchema: map[string]interface{}{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tools[0].Parameters.(map[string]interface{}); !ok {
		t.Fatalf("map InputSchema: Parameters = %T", tools[0].Parameters)
	}
}

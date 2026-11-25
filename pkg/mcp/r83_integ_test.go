package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
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

// r83integValidator is a bare schema.Validator (not a schema.Schema).
type r83integValidator struct{}

func (r83integValidator) Validate(interface{}) error { return errors.New("r83integ: rejected") }
func (r83integValidator) JSONSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
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

	// A bare validator is kept too (its Validate checks tool input).
	tools, err = converter.ToolsFromDefinitions(defs, map[string]MCPToolSchema{"weather": {InputSchema: r83integValidator{}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tools[0].Parameters.(r83integValidator); !ok {
		t.Fatalf("validator InputSchema: Parameters = %T", tools[0].Parameters)
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

// TestR83integWebhookBodyDecodedLikeTextDecoder: TS mcp-event-webhook.ts
// parses safeParseJSON({text: new TextDecoder().decode(body)}), so a signed
// body with a UTF-8 BOM is a valid event (Go's JSON decoder rejects the BOM).
func TestR83integWebhookBodyDecodedLikeTextDecoder(t *testing.T) {
	h := newWebhookHarness(t)
	raw, _ := json.Marshal(testEvent)
	body := append([]byte{0xEF, 0xBB, 0xBF}, raw...)
	r := signedEventRequestBody(h.sub, body, "evt_1", fmt.Sprint(mcpEventNow().Unix()), h.sub.Delivery.Secret)
	if code := h.do(r); code != 204 || h.events != 1 {
		t.Fatalf("code=%d events=%d, want 204 and one event", code, h.events)
	}
}

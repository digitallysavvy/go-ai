package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

const orderedToolSchema = `{"type":"object","required":["zeta","alpha"],"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`

func hintedTool(t *testing.T) types.Tool {
	t.Helper()
	hint, ok := jsonorder.Parse([]byte(orderedToolSchema))
	if !ok {
		t.Fatal("parse")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(orderedToolSchema), &m); err != nil {
		t.Fatal(err)
	}
	return types.Tool{Name: "t", Parameters: jsonorder.Hinted{V: m, Hint: hint}}
}

// A tool schema keeps its key order across the durable boundary (serialize,
// JSON text, deserialize, resolve), like TS where the JSON text keeps order.
func TestSerializableToolKeepsSchemaKeyOrder(t *testing.T) {
	defs, err := SerializeToolSet([]types.Tool{hintedTool(t)})
	if err != nil {
		t.Fatal(err)
	}
	text, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), `"parameters":`+orderedToolSchema) || !strings.Contains(string(text), `"inputSchema":`+orderedToolSchema) {
		t.Fatalf("serialized text lost order: %s", text)
	}
	var back map[string]SerializableToolDef
	if err := json.Unmarshal(text, &back); err != nil {
		t.Fatal(err)
	}
	resolved := ResolveSerializableTools(back)
	got, err := json.Marshal(resolved[0].Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orderedToolSchema {
		t.Fatalf("resolved schema = %s\nwant            %s", got, orderedToolSchema)
	}
	// Still validates and still reads as a map.
	if m, ok := schema.ParametersMap(resolved[0].Parameters); !ok || m["type"] != "object" {
		t.Fatalf("ParametersMap = %#v", m)
	}
	if _, err := ValidateSerializableToolInput(back["t"], map[string]interface{}{"zeta": "a", "alpha": "b"}); err != nil {
		t.Fatal(err)
	}
}

func TestSerializeOrderedSchemaToolHasNoOwnValidator(t *testing.T) {
	s, err := schema.NewOrderedSchema([]byte(orderedToolSchema))
	if err != nil {
		t.Fatal(err)
	}
	defs, err := SerializeToolSet([]types.Tool{{Name: "o", Parameters: s}})
	if err != nil {
		t.Fatal(err)
	}
	if defs["o"].HasOwnValidator {
		t.Fatal("an OrderedSchema is plain JSON Schema: HasOwnValidator must be false")
	}
	text, _ := json.Marshal(defs["o"])
	if !strings.Contains(string(text), `"parameters":`+orderedToolSchema) {
		t.Fatalf("lost order: %s", text)
	}
}

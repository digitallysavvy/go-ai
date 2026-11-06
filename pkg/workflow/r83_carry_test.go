package workflow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type r83carryWeatherInput struct {
	Zip     string `json:"zip" validate:"required"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

// r83carryRoundTrip serializes tools, sends them across a JSON boundary and
// resolves them, as a durable workflow step does.
func r83carryRoundTrip(t *testing.T, tools []types.Tool) ([]byte, []types.Tool) {
	t.Helper()
	defs, err := SerializeToolSet(tools)
	if err != nil {
		t.Fatalf("SerializeToolSet: %v", err)
	}
	raw, err := json.Marshal(defs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]SerializableToolDef
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return raw, ResolveSerializableTools(back)
}

// TS serializeToolSet stores asSchema(zodSchema).jsonSchema, whose properties
// keep the zod object's key order, and that JSON text keeps it across the
// step boundary. A Go struct schema (field order) must keep it too.
func TestR83carryStructSchemaKeepsFieldOrderAcrossBoundary(t *testing.T) {
	for name, params := range map[string]interface{}{
		"SimpleStructSchema": schema.NewSimpleStructSchema(reflect.TypeOf(r83carryWeatherInput{})),
		"StructValidator":    schema.NewStructSchema(reflect.TypeOf(r83carryWeatherInput{})),
	} {
		t.Run(name, func(t *testing.T) {
			raw, tools := r83carryRoundTrip(t, []types.Tool{{Name: "weather", Parameters: params}})
			s := string(raw)
			zi, ci, ki := strings.Index(s, `"zip"`), strings.Index(s, `"city"`), strings.Index(s, `"country"`)
			if zi < 0 || zi >= ci || ci >= ki {
				t.Fatalf("serialized definition lost the field order: %s", s)
			}
			ordered, ok := schema.OrderedToolParameters(tools[0].Parameters, tools[0].ParameterKeyOrder)
			if !ok {
				t.Fatalf("resolved tool has no key order: %#v", tools[0].ParameterKeyOrder)
			}
			out, err := ordered.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			want, _ := json.Marshal(params)
			if string(out) != string(want) {
				t.Fatalf("resolved schema\n got %s\nwant %s", out, want)
			}
		})
	}
}

// A struct schema validates with its own validator (TS: zod has validate, so
// hasOwnValidator), also when Parameters is the bare *schema.StructValidator,
// which the agent loop accepts as a tool schema.
func TestR83carryBareValidatorHasOwnValidator(t *testing.T) {
	defs, err := SerializeToolSet([]types.Tool{{Name: "weather", Parameters: schema.NewStructSchema(reflect.TypeOf(r83carryWeatherInput{}))}})
	if err != nil {
		t.Fatal(err)
	}
	def := defs["weather"]
	if !def.HasOwnValidator {
		t.Fatalf("bare StructValidator: hasOwnValidator = false")
	}
	if def.InputSchema["type"] != "object" {
		t.Fatalf("inputSchema = %#v", def.InputSchema)
	}
	plain, err := SerializeToolSet([]types.Tool{{Name: "p", Parameters: schema.NewJSONSchema(map[string]interface{}{"type": "object"})}})
	if err != nil {
		t.Fatal(err)
	}
	if plain["p"].HasOwnValidator {
		t.Fatalf("bare JSONSchemaValidator must be validated as JSON Schema")
	}
}

// A schema value that is no schema (typed nil or zero value) is no input
// schema (TS asSchema(undefined)), not a panic.
func TestR83carryNilSchemaValues(t *testing.T) {
	var nilSimple *schema.SimpleJSONSchema
	var nilStruct *schema.StructValidator
	for name, params := range map[string]interface{}{
		"typed nil SimpleJSONSchema": nilSimple,
		"zero SimpleJSONSchema":      &schema.SimpleJSONSchema{},
		"zero SimpleStructSchema":    &schema.SimpleStructSchema{},
		"typed nil StructValidator":  nilStruct,
	} {
		t.Run(name, func(t *testing.T) {
			defs, err := SerializeToolSet([]types.Tool{{Name: "t", Parameters: params}})
			if err != nil {
				t.Fatal(err)
			}
			if defs["t"].InputSchema != nil || defs["t"].HasOwnValidator {
				t.Fatalf("def = %#v", defs["t"])
			}
		})
	}
}

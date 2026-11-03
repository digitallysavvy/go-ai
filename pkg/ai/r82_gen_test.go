package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r82genNilSchemaValidator is a Schema whose Validator is nil.
type r82genNilSchemaValidator struct{}

func (r82genNilSchemaValidator) Validator() schema.Validator { return nil }

// TS parseToolCall's repair inputSchema callback is
// asSchema(getOwn(tools, toolName)?.inputSchema).jsonSchema: a tool without an
// input schema has the empty object schema. A nil schema value (a typed nil
// Schema or Validator pointer, a Schema whose Validator is nil, a nil map) is
// no schema when a call is parsed; the repair callback dereferenced it and
// panicked.
func TestR82genRepairInputSchemaNilSchemaKinds(t *testing.T) {
	cases := []struct {
		name   string
		params interface{}
	}{
		{"typed nil SimpleJSONSchema", (*schema.SimpleJSONSchema)(nil)},
		{"typed nil SimpleStructSchema", (*schema.SimpleStructSchema)(nil)},
		{"typed nil JSONSchemaValidator", (*schema.JSONSchemaValidator)(nil)},
		{"typed nil OrderedSchema", (*schema.OrderedSchema)(nil)},
		{"zero SimpleJSONSchema", &schema.SimpleJSONSchema{}},
		{"schema with nil validator", r82genNilSchemaValidator{}},
		{"nil map", map[string]interface{}(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := types.Tool{Name: "weather", Parameters: tc.params}
			var got map[string]interface{}
			var schemaErr error
			_, err := ParseToolCall(context.Background(), ParseToolCallOptions{
				ToolCall: types.ToolCall{ID: "call-1", ToolName: "wether", RawArguments: `{}`},
				Tools:    []types.Tool{tool},
				RepairToolCall: func(_ context.Context, opts ToolCallRepairOptions) (*types.ToolCall, error) {
					got, schemaErr = opts.InputSchema("weather")
					return nil, nil
				},
			})
			if err != nil {
				t.Fatalf("ParseToolCall: %v", err)
			}
			if schemaErr != nil {
				t.Fatalf("InputSchema: %v", schemaErr)
			}
			if !reflect.DeepEqual(got, emptyToolInputJSONSchema()) {
				t.Fatalf("InputSchema = %v, want the empty object schema", got)
			}
		})
	}
}

// A struct-tag schema's repair input schema is its JSONSchema (owner decision
// 1 is pending; this only pins that the callback reaches it).
func TestR82genRepairInputSchemaStructSchema(t *testing.T) {
	tool := types.Tool{Name: "weather", Parameters: schema.NewSimpleStructSchema(reflect.TypeOf(r81genStructInput{}))}
	var got map[string]interface{}
	_, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "call-1", ToolName: "wether", RawArguments: `{}`},
		Tools:    []types.Tool{tool},
		RepairToolCall: func(_ context.Context, opts ToolCallRepairOptions) (*types.ToolCall, error) {
			got, _ = opts.InputSchema("weather")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("ParseToolCall: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]interface{}{"type": "object"}) {
		t.Fatalf("InputSchema = %v", got)
	}
}

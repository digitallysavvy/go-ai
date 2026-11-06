package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS repairToolCall's inputSchema is asSchema(getOwn(tools, toolName)
// ?.inputSchema).jsonSchema: a name with no tool gets asSchema(undefined), the
// empty object schema, rather than an error.
func TestRepairInputSchemaForUnknownToolIsEmptyObjectSchema(t *testing.T) {
	var schema map[string]interface{}
	var schemaErr error
	_, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "1", ToolName: "unknownTool", RawArguments: `{}`},
		Tools:    []types.Tool{pipelineTestTool("testTool", nil)},
		RepairToolCall: func(_ context.Context, opts ToolCallRepairOptions) (*types.ToolCall, error) {
			schema, schemaErr = opts.InputSchema("unknownTool")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	if schemaErr != nil || !reflect.DeepEqual(schema, want) {
		t.Fatalf("InputSchema(unknownTool) = %v, %v; want %v", schema, schemaErr, want)
	}
}

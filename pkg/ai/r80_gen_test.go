package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r80genDeferredResultParts are the forms a model response can answer a
// provider-executed call with: every tool-result or tool-error part is a
// provider result (TS convertLanguageModelContent).
func r80genDeferredResultParts(id string) map[string]types.ContentPart {
	return map[string]types.ContentPart{
		"tool-result":          types.ToolResultContent{ToolCallID: id, ToolName: "code_execution", Result: "ok"},
		"tool-result pointer":  &types.ToolResultContent{ToolCallID: id, ToolName: "code_execution", Result: "ok"},
		"tool-error":           types.ToolErrorContent{ToolCallID: id, ToolName: "code_execution", Error: "failed"},
		"tool-error pointer":   &types.ToolErrorContent{ToolCallID: id, ToolName: "code_execution", Error: "failed"},
		"tool-error unflagged": types.ToolErrorContent{ToolCallID: id, ToolName: "code_execution", Error: "failed", ProviderExecuted: false},
	}
}

// A deferred provider call answered in the same response is not pending (TS
// pendingDeferredToolCalls deletes every tool-result part's ID), so the run
// ends after one step. GenerateText counted only a ToolResultContent value;
// a tool-error part (or a pointer part) left the call pending and the loop
// called the model again, while the legacy loop ended.
func TestR80GenDeferredCallResolvedByAnyResultPart(t *testing.T) {
	for name, part := range r80genDeferredResultParts("call-1") {
		t.Run(name, func(t *testing.T) {
			calls := 0
			model := &testutil.MockLanguageModel{
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					calls++
					return &types.GenerateResult{
						ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "code_execution", Arguments: map[string]interface{}{}, ProviderExecuted: true}},
						Content: []types.ContentPart{
							types.ToolCallContent{ToolCallID: "call-1", ToolName: "code_execution", Input: "{}", ProviderExecuted: true},
							part,
						},
						FinishReason: types.FinishReasonStop,
					}, nil
				},
			}
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:    model,
				Prompt:   "x",
				Tools:    []types.Tool{{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true}},
				StopWhen: []StopCondition{StepCountIs(3)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(result.Steps) != 1 {
				t.Fatalf("model calls = %d, steps = %d, want 1 and 1", calls, len(result.Steps))
			}
		})
	}
}

// TS repair inputSchema is asSchema(getOwn(tools, toolName)?.inputSchema)
// .jsonSchema: a tool without an input schema has the empty object schema
// {type: object, properties: {}, additionalProperties: false}, as an unknown
// tool does. Go returned {"type":"object"} for a tool without Parameters.
func TestR80GenRepairInputSchemaWithoutParameters(t *testing.T) {
	want := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	var got, unknown map[string]interface{}
	_, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "c1", ToolName: "t", RawArguments: "invalid json"},
		Tools:    []types.Tool{{Name: "t"}},
		RepairToolCall: func(ctx context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
			var err error
			if got, err = o.InputSchema("t"); err != nil {
				return nil, err
			}
			unknown, err = o.InputSchema("missing")
			return nil, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(unknown, want) {
		t.Fatalf("input schema = %v, unknown tool = %v, want %v", got, unknown, want)
	}
}

package ai

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r76genErrorValueTool returns a Go error as its result value (not as its
// error): TS execute returning an Error object.
func r76genErrorValueTool() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return errors.New("boom"), nil
		},
	}
}

// r76genCheckErrorValueStep checks a step whose tool returned a Go error as
// its result value: TS content is a tool-result whose output is the value,
// step.toolResults holds it, and the tool message carries
// createToolModelOutput's json toJSONValue(output) ({} for an Error).
func r76genCheckErrorValueStep(t *testing.T, step types.StepResult) {
	t.Helper()
	var result *types.ToolResultContent
	for _, part := range step.Content {
		switch p := part.(type) {
		case types.ToolErrorContent:
			t.Fatalf("content has a tool-error %#v, want a tool-result", p)
		case types.ToolResultContent:
			result = &p
		}
	}
	if result == nil {
		t.Fatalf("no tool-result in %#v", step.Content)
	}
	if err, ok := result.Result.(error); !ok || err.Error() != "boom" {
		t.Fatalf("Result = %#v, want the raw error value", result.Result)
	}
	want := &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{}}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("content Output = %#v, want %#v", result.Output, want)
	}
	if len(step.ToolResults) != 1 {
		t.Fatalf("ToolResults = %d, want 1", len(step.ToolResults))
	}
	msgs := step.Response.Messages
	if len(msgs) != 2 || msgs[1].Role != types.RoleTool {
		t.Fatalf("response messages %#v", msgs)
	}
	got, ok := msgs[1].Content[0].(types.ToolResultContent)
	if !ok || !reflect.DeepEqual(got.Output, want) {
		t.Fatalf("tool message part = %#v, want Output %#v", msgs[1].Content[0], want)
	}
}

// A tool that returns a Go error as its result value (not as its error) is a
// tool result, as TS treats an execute that returns an Error object. Go used
// to record a tool-error content part and an error-text model output while
// still counting the call in ToolResults and streaming a tool-result chunk.
func TestR76Gen_ToolReturningErrorValueIsToolResult(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}}
	t.Run("generate", func(t *testing.T) {
		var n atomic.Int32
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if n.Add(1) == 1 {
				return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{r76genErrorValueTool()}, StopWhen: []StopCondition{StepCountIs(2)}})
		if err != nil {
			t.Fatal(err)
		}
		r76genCheckErrorValueStep(t, res.Steps[0])
	})
	t.Run("stream", func(t *testing.T) {
		step1 := []provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &call}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}
		step2 := []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1, step2), Prompt: "test-input", Tools: []types.Tool{r76genErrorValueTool()}, StopWhen: []StopCondition{StepCountIs(2)}})
		r76genCheckErrorValueStep(t, r.Steps()[0])
	})
	t.Run("builders", func(t *testing.T) {
		tr := types.ToolResult{ToolCallID: "call-1", ToolName: "tool1", Result: errors.New("boom")}
		for _, part := range []types.ContentPart{toolResultsToContentParts([]types.ToolResult{tr})[0], toolResultContentFromToolResult(tr)} {
			if _, ok := part.(types.ToolResultContent); !ok {
				t.Fatalf("part = %#v, want a tool-result", part)
			}
		}
	})
}

// R76 item 2 (carry CR cr-r76-carry-to-gen.md): with withoutCallMetadata
// (set from InternalOptions.ClientToolResultsWithoutCallMetadata for
// WorkflowAgent), executed, failed and invalid client tool results carry no
// provider metadata (TS WorkflowAgent executeTool/createInvalidToolResult);
// without it the call's metadata is copied (TS core execute-tool-call.ts).
func TestR76Gen_ClientToolResultsWithoutCallMetadata(t *testing.T) {
	md := map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig"}}
	exec := func(out interface{}, err error) types.ToolExecutor {
		return func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return out, err
		}
	}
	tools := []types.Tool{
		{Name: "ok", Parameters: map[string]interface{}{"type": "object"}, Execute: exec("ok", nil)},
		{Name: "fail", Parameters: map[string]interface{}{"type": "object"}, Execute: exec(nil, errors.New("boom"))},
	}
	calls := []types.ToolCall{
		{ID: "c1", ToolName: "ok", Arguments: map[string]interface{}{}, ProviderMetadata: md},
		{ID: "c2", ToolName: "fail", Arguments: map[string]interface{}{}, ProviderMetadata: md},
		{ID: "c3", ToolName: "ok", Invalid: true, Error: errors.New("bad input"), ProviderMetadata: md},
	}
	for _, without := range []bool{false, true} {
		results, err := executeTools(context.Background(), calls, tools, nil, nil, nil, &types.Usage{}, toolCallEventCallbacks{withoutCallMetadata: without})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 3 {
			t.Fatalf("%d results", len(results))
		}
		for _, r := range results {
			if without && r.ProviderMetadata != nil {
				t.Errorf("without: %s ProviderMetadata = %v, want nil", r.ToolCallID, r.ProviderMetadata)
			}
			if !without && !reflect.DeepEqual(r.ProviderMetadata, md) {
				t.Errorf("default: %s ProviderMetadata = %v, want the call's", r.ToolCallID, r.ProviderMetadata)
			}
		}
	}
}

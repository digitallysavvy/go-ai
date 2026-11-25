package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 99 gen tests (TS 2ebc78b654).

// A tool call whose input holds __proto__ fails TS parseToolCall
// (safeParseJSON), so the invalid call's input is the raw text and
// toResponseMessages replays it as {} (`part.invalid && typeof part.input
// !== 'object' ? {} : part.input`), whatever arguments the provider decoded.
func TestR99genInvalidProtoToolCallReplaysEmptyInput(t *testing.T) {
	for _, withArgs := range []bool{false, true} {
		call := types.ToolCall{ID: "c1", ToolName: "t", RawArguments: `{"__proto__":{"a":1}}`}
		if withArgs {
			call.Arguments = map[string]interface{}{"__proto__": map[string]interface{}{"a": 1.0}}
		}
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{{
			Name: "t", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		assistant := res.ResponseMessages[0]
		if got := assistant.ToolCalls[0].Arguments; !reflect.DeepEqual(got, map[string]interface{}{}) {
			t.Errorf("withArgs=%v: replayed tool call input = %#v, want {}", withArgs, got)
		}
		for _, part := range assistant.Content {
			if tc, ok := part.(types.ToolCallContent); ok && !reflect.DeepEqual(tc.Arguments, map[string]interface{}{}) {
				t.Errorf("withArgs=%v: replayed tool-call part input = %#v, want {}", withArgs, tc.Arguments)
			}
		}
	}
}

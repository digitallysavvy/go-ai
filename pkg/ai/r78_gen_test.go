package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r78genTitledTool has a title, metadata and a required string value; its
// Execute fails when value is "boom".
func r78genTitledTool() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Title:      "Tool One",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Metadata:   map[string]interface{}{"owner": "team"},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			if in["value"] == "boom" {
				return nil, errors.New("exec failed")
			}
			return "ok", nil
		},
	}
}

func r78genToolErrors(parts []types.ContentPart) map[string]types.ToolErrorContent {
	out := map[string]types.ToolErrorContent{}
	for _, p := range parts {
		if te, ok := p.(types.ToolErrorContent); ok {
			out[te.ToolCallID] = te
		}
	}
	return out
}

// Ruling (c)(1): TS streamLanguageModelCall's tool-error for an invalid
// client call carries `title: toolCall.title` and the toolMetadata
// (stream-language-model-call.ts 746-756); an executed failure has no title
// (execute-tool-call.ts 163-175); generateText's invalid tool-error has
// neither (generate-text.ts 1484-1492).
func TestR78Gen_InvalidToolErrorTitle(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		step1 := []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "bad", ToolName: "tool1", RawArguments: `{}`}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "fail", ToolName: "tool1", RawArguments: `{"value":"boom"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1, r71streamHelloWorld), Prompt: "x", Tools: []types.Tool{r78genTitledTool()}})
		errs := r78genToolErrors(r.Steps()[0].Content)
		bad, fail := errs["bad"], errs["fail"]
		if bad.Title != "Tool One" || bad.ToolMetadata["owner"] != "team" || !bad.Dynamic {
			t.Fatalf("invalid tool error = %#v, want title, metadata, dynamic", bad)
		}
		if fail.ToolCallID != "fail" || fail.Title != "" {
			t.Fatalf("executed failure = %#v, want no title", fail)
		}
	})
	t.Run("stream with transform", func(t *testing.T) {
		// Content recorded from the transformed stream
		// (toolResultContentFromToolResult) keeps the title as well.
		step1 := []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "bad", ToolName: "tool1", RawArguments: `{}`}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "fail", ToolName: "tool1", RawArguments: `{"value":"boom"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}
		identity := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk { return []provider.StreamChunk{c} }
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1, r71streamHelloWorld), Prompt: "x", Tools: []types.Tool{r78genTitledTool()}, ExperimentalTransform: []StreamTransformFunc{identity}})
		errs := r78genToolErrors(r.Steps()[0].Content)
		if bad := errs["bad"]; bad.Title != "Tool One" {
			t.Fatalf("invalid tool error = %#v, want title", bad)
		}
		if fail := errs["fail"]; fail.ToolCallID != "fail" || fail.Title != "" {
			t.Fatalf("executed failure = %#v, want no title", fail)
		}
	})
	t.Run("generate", func(t *testing.T) {
		calls := 0
		model := &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{
				{ID: "bad", ToolName: "tool1", RawArguments: `{}`},
				{ID: "fail", ToolName: "tool1", RawArguments: `{"value":"boom"}`},
			}}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{r78genTitledTool()}})
		if err != nil {
			t.Fatal(err)
		}
		errs := r78genToolErrors(res.Steps[0].Content)
		if bad := errs["bad"]; bad.ToolCallID != "bad" || bad.Title != "" || bad.ToolMetadata != nil {
			t.Fatalf("invalid tool error = %#v, want no title and no metadata", bad)
		}
		if fail := errs["fail"]; fail.ToolCallID != "fail" || fail.Title != "" {
			t.Fatalf("executed failure = %#v, want no title", fail)
		}
	})
}

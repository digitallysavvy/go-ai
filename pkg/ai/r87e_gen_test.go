package ai

// Round 87e gen: known differences that are now fixed (TS 3fab50aa9b). Each
// test names the TS source it follows.

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r87egenTitledTool is a local tool with a title (TS tool({title, execute})).
func r87egenTitledTool() types.Tool {
	return types.Tool{
		Name:       "weather",
		Title:      "Weather",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
}

// TS execute-tool-call.ts builds the tool-result of a client tool without
// title (only the tool call and a preliminary result, `...toolCall`, have
// it). Known difference "Executed tool results carry the tool title".
func TestR87egenExecutedToolResultHasNoTitle(t *testing.T) {
	t.Parallel()
	call := types.ToolCall{ID: "c1", ToolName: "weather", RawArguments: `{"city":"Paris"}`}
	var mu sync.Mutex
	var finish []OnToolCallFinishEvent
	n := 0
	gen, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			n++
			if n == 1 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{call}}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}},
		Prompt:   "go",
		Tools:    []types.Tool{r87egenTitledTool()},
		StopWhen: []StopCondition{StepCountIs(2)},
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			mu.Lock()
			finish = append(finish, e)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got := gen.Steps[0].ToolCalls[0].Title; got != "Weather" {
		t.Errorf("tool call title = %q, want Weather", got)
	}
	if got := gen.Steps[0].ToolResults[0].Title; got != "" {
		t.Errorf("GenerateText tool result title = %q, want none", got)
	}
	mu.Lock()
	if len(finish) != 1 || finish[0].ToolOutput.Title != "" || finish[0].ToolCall.Title != "Weather" {
		t.Errorf("OnToolCallFinish = %+v, want an output without title and the titled call", finish)
	}
	mu.Unlock()

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &call},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}},
		Prompt: "go",
		Tools:  []types.Tool{r87egenTitledTool()},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	sawResult := false
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
			sawResult = true
			if chunk.ToolResult.Title != "" {
				t.Errorf("StreamText tool-result part title = %q, want none", chunk.ToolResult.Title)
			}
		}
	}
	if !sawResult {
		t.Fatal("StreamText sent no tool-result part")
	}
}

// TS convert-language-model-content.ts turns a model tool-result with isError
// into a tool-error whose error is part.result: undefined when the result has
// no value, so the response message replays it as error-json null
// (createToolModelOutput toJSONValue(undefined)). Known difference "Provider
// tool error without a value".
func TestR87egenProviderToolErrorWithoutValue(t *testing.T) {
	t.Parallel()
	isErr := true
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", IsError: &isErr, ProviderExecuted: true},
			},
			ToolCalls:    []types.ToolCall{{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var errPart *types.ToolErrorContent
	for _, part := range res.FinalStep.Content {
		if p, ok := part.(types.ToolErrorContent); ok {
			errPart = &p
		}
	}
	if errPart == nil || errPart.Error != nil {
		t.Fatalf("tool-error part = %+v, want one with a nil Error", errPart)
	}
	msgs := res.FinalStep.Response.Messages
	if len(msgs) != 1 || len(msgs[0].Content) != 2 {
		t.Fatalf("response messages = %+v, want one assistant message with the call and its result", msgs)
	}
	replayed, ok := msgs[0].Content[1].(types.ToolResultContent)
	if !ok || replayed.Output == nil || replayed.Output.Type != types.ToolResultOutputErrorJSON || replayed.Output.Value != nil {
		t.Fatalf("replayed tool result = %+v, want error-json null", msgs[0].Content[1])
	}

	// The stream path's ToolResult carries errProviderToolNoValue (a Go
	// error must be non-nil); its content part keeps the error undefined.
	part := toolResultContentFromToolResult(types.ToolResult{ToolCallID: "ws", ToolName: "web_search", Error: errProviderToolNoValue, ProviderExecuted: true})
	streamed, ok := part.(types.ToolErrorContent)
	if !ok || streamed.Error != nil || streamed.Err != nil {
		t.Fatalf("streamed tool-error part = %+v, want a nil Error and Err", part)
	}
}

package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func noToolsModel() *testutil.MockLanguageModel {
	call := types.ToolCall{ID: "c1", ToolName: "foo", RawArguments: "{}", Arguments: map[string]interface{}{}}
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{call}}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			c := call
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &c},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
}

func hasToolMessageError(msgs []types.Message) bool {
	for _, m := range msgs {
		if m.Role != types.RoleTool {
			continue
		}
		for _, p := range m.Content {
			if tr, ok := p.(types.ToolResultContent); ok && tr.ToolCallID == "c1" {
				return true
			}
		}
	}
	return false
}

// An invalid (NoSuchTool) call is answered with a tool-error output even when
// no tools were given (TS generateText: clientToolOutputs for invalid calls).
func TestGenerateText_NoToolsInvalidCallGetsToolErrorOutput(t *testing.T) {
	t.Parallel()
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: noToolsModel(), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var sawErr bool
	for _, p := range res.Content {
		if _, ok := p.(types.ToolErrorContent); ok {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("content has no tool-error part: %#v", res.Content)
	}
	if !hasToolMessageError(res.ResponseMessages) {
		t.Fatalf("response messages lack the tool message for the invalid call: %#v", res.ResponseMessages)
	}
}

func TestStreamText_NoToolsInvalidCallGetsToolErrorOutput(t *testing.T) {
	t.Parallel()
	res, err := StreamText(context.Background(), StreamTextOptions{Model: noToolsModel(), Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if !hasToolMessageError(res.ResponseMessages()) {
		t.Fatalf("response messages lack the tool message for the invalid call: %#v", res.ResponseMessages())
	}
}

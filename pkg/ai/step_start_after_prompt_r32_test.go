package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS converts the step prompt before it fires onStepStart, so a prompt that
// fails to convert (here a tool call without a result) never produces a
// step-start event.
func TestStepStartNotFiredWhenPromptConversionFails(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: "{}"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
	}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "x", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var missing *types.MissingToolResultsError

	started := false
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Messages: msgs,
		OnStepStart: func(context.Context, OnStepStartEvent) { started = true },
	})
	if !errors.As(err, &missing) {
		t.Fatalf("GenerateText err = %v, want MissingToolResultsError", err)
	}
	if started {
		t.Error("GenerateText fired OnStepStart before the prompt converted")
	}

	started = false
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Messages: msgs,
		OnStepStart: func(context.Context, OnStepStartEvent) { started = true },
	})
	if err == nil && res != nil {
		_, err = res.ReadAll()
	}
	if !errors.As(err, &missing) {
		t.Fatalf("StreamText err = %v, want MissingToolResultsError", err)
	}
	if started {
		t.Error("StreamText fired OnStepStart before the prompt converted")
	}
}

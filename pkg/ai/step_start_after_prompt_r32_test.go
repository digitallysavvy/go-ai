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

// The streamText step-end event carries the response id and model like the
// step result (TS onStepEnd event is the step result).
func TestStreamTextStepEndEventCarriesResponseMetadata(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "resp-9", ModelID: "served"}},
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var got OnStepFinishEvent
	var end OnFinishEvent
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "x",
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { got = e },
		OnEndEvent:     func(_ context.Context, e OnFinishEvent) { end = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got.Response.ID != "resp-9" || got.Response.ModelID != "served" {
		t.Fatalf("step-end response = %+v", got.Response)
	}
	if end.Response.ID != "resp-9" || end.Response.ModelID != "served" {
		t.Fatalf("end response = %+v", end.Response)
	}
}

package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var r65UserInstruction = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}

// TS standardizePrompt throws InvalidPromptError("instructions must be a
// string, SystemModelMessage, or array of SystemModelMessage"). The v0.5
// *InvalidArgumentError stays reachable through errors.As.
func TestR65InvalidInstructionsIsInvalidPromptError(t *testing.T) {
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "hi", InstructionMessages: r65UserInstruction})
	var promptErr *providererrors.InvalidPromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("err = %T %v, want *InvalidPromptError", err, err)
	}
	if got, want := err.Error(), "Invalid prompt: instructions must be a string, SystemModelMessage, or array of SystemModelMessage"; got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) || argErr.Field != "instructions" {
		t.Fatalf("errors.As InvalidArgumentError = %v", argErr)
	}
	_, err = StreamText(context.Background(), StreamTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "hi", InstructionMessages: r65UserInstruction})
	if !errors.As(err, &promptErr) {
		t.Fatalf("StreamText err = %T %v, want *InvalidPromptError", err, err)
	}
}

// TS standardizePrompt reports a missing prompt before invalid instructions,
// and invalid instructions before empty messages.
func TestR65GenerateTextPromptChecksInStandardizePromptOrder(t *testing.T) {
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: &testutil.MockLanguageModel{}, InstructionMessages: r65UserInstruction})
	if err == nil || err.Error() != "Invalid prompt: prompt or messages must be defined" {
		t.Fatalf("no prompt: err = %v", err)
	}
	_, err = GenerateText(context.Background(), GenerateTextOptions{Model: &testutil.MockLanguageModel{}, Messages: []types.Message{}, InstructionMessages: r65UserInstruction})
	if err == nil || err.Error() != "Invalid prompt: instructions must be a string, SystemModelMessage, or array of SystemModelMessage" {
		t.Fatalf("empty messages: err = %v", err)
	}
}

// TS TextBatchRequest is Prompt & ...: instructions (a string or system
// messages with provider options) reach the provider ahead of the messages.
func TestR65BatchTextRequestInstructions(t *testing.T) {
	var got provider.BatchV4StartOptions
	api := &mockBatchV4{providerName: "mock.batch", startFn: func(_ context.Context, o provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
		got = o
		return &provider.BatchV4StartResult{BatchID: "b1"}, nil
	}}
	instructions := "be brief"
	sysMessages := []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "cached"}}, ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}}}
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{Provider: api, Requests: []BatchRequest{
		{Text: &BatchTextRequest{ID: "a", Model: "m", Prompt: "hi", System: "ignored", Instructions: &instructions}},
		{Text: &BatchTextRequest{ID: "b", Model: "m", Prompt: "hi", System: "ignored", InstructionMessages: sysMessages}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if p := got.Requests[0].Text.Options.Prompt; p.System != "be brief" {
		t.Fatalf("request a system = %q, want the instructions", p.System)
	}
	p := got.Requests[1].Text.Options.Prompt
	if p.System != "" || len(p.Messages) != 2 || p.Messages[0].Role != types.RoleSystem || p.Messages[0].ProviderOptions["anthropic"] == nil {
		t.Fatalf("request b prompt = %+v, want the instruction message first with its provider options", p)
	}

	_, err = ExperimentalStartBatch(context.Background(), StartBatchOptions{Provider: api, Requests: []BatchRequest{
		{Text: &BatchTextRequest{ID: "a", Model: "m", Prompt: "hi", InstructionMessages: r65UserInstruction}},
	}})
	var promptErr *providererrors.InvalidPromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("user-role instruction: err = %v, want InvalidPromptError", err)
	}
}

// TS startBatch runs standardizePrompt before prepareTools and
// prepareLanguageModelCallOptions: a system message without the opt-in is
// reported before an invalid maxOutputTokens or a conflicting tool.
func TestR65BatchSystemMessageCheckedBeforeToolsAndSettings(t *testing.T) {
	api := &mockBatchV4{providerName: "mock.batch", startFn: func(context.Context, provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
		return &provider.BatchV4StartResult{BatchID: "b1"}, nil
	}}
	zero := 0
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "s"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	toolA := types.Tool{Name: "t", Description: "a", Parameters: map[string]interface{}{"type": "object"}}
	toolB := types.Tool{Name: "t", Description: "b", Parameters: map[string]interface{}{"type": "object"}}
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{Provider: api, Requests: []BatchRequest{
		{Text: &BatchTextRequest{ID: "a", Model: "m", Prompt: "hi", Tools: []types.Tool{toolA}}},
		{Text: &BatchTextRequest{ID: "b", Model: "m", Messages: messages, MaxTokens: &zero, Tools: []types.Tool{toolB}}},
	}})
	var promptErr *providererrors.InvalidPromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("err = %v, want the system message InvalidPromptError", err)
	}
}

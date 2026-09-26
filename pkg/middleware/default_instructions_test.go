package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ported from middleware/default-instructions-middleware.test.ts (ai@7.0.113).

func baseGenerateOptions() *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello, world!"}}},
			},
		},
	}
}

func TestDefaultInstructionsMiddleware_PrependsStringInstructions(t *testing.T) {
	t.Parallel()
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{Instructions: "You are a helpful assistant."})
	params := baseGenerateOptions()
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if got.Prompt.System != "You are a helpful assistant." {
		t.Fatalf("Prompt.System = %q", got.Prompt.System)
	}
	if len(got.Prompt.Messages) != 1 {
		t.Fatalf("Prompt.Messages = %+v", got.Prompt.Messages)
	}
}

func TestDefaultInstructionsMiddleware_PreservesProviderOptionsOnDefaultInstructions(t *testing.T) {
	t.Parallel()
	providerOpts := map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}}, ProviderOptions: providerOpts},
		},
	})
	params := baseGenerateOptions()
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if len(got.Prompt.Messages) != 2 || got.Prompt.Messages[0].Role != types.RoleSystem {
		t.Fatalf("Prompt.Messages = %+v", got.Prompt.Messages)
	}
	if got.Prompt.Messages[0].ProviderOptions["anthropic"] == nil {
		t.Fatalf("expected provider options to be preserved, got %+v", got.Prompt.Messages[0].ProviderOptions)
	}
}

func TestDefaultInstructionsMiddleware_PrependsMultipleMessagesInOrder(t *testing.T) {
	t.Parallel()
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}}},
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Answer concisely."}}},
		},
	})
	params := baseGenerateOptions()
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if len(got.Prompt.Messages) != 3 {
		t.Fatalf("Prompt.Messages = %+v", got.Prompt.Messages)
	}
	first := got.Prompt.Messages[0].Content[0].(types.TextContent).Text
	second := got.Prompt.Messages[1].Content[0].(types.TextContent).Text
	if first != "You are a helpful assistant." || second != "Answer concisely." {
		t.Fatalf("unexpected order: %q, %q", first, second)
	}
}

func TestDefaultInstructionsMiddleware_CallLevelSystemTakesPrecedence(t *testing.T) {
	t.Parallel()
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{Instructions: "Default instructions"})
	params := baseGenerateOptions()
	params.Prompt.System = "Call-level instructions"
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if got != params {
		t.Fatalf("expected params to be returned unchanged (same pointer)")
	}
	if got.Prompt.System != "Call-level instructions" {
		t.Fatalf("Prompt.System = %q", got.Prompt.System)
	}
}

func TestDefaultInstructionsMiddleware_SystemMessageLaterInPromptStillCounts(t *testing.T) {
	t.Parallel()
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{Instructions: "Default instructions"})
	params := baseGenerateOptions()
	params.Prompt.Messages = append(params.Prompt.Messages, types.Message{
		Role:    types.RoleSystem,
		Content: []types.ContentPart{types.TextContent{Text: "Trusted conversation instructions"}},
	})
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if got != params {
		t.Fatalf("expected params to be returned unchanged (same pointer)")
	}
}

func TestDefaultInstructionsMiddleware_PreservesOtherCallParameters(t *testing.T) {
	t.Parallel()
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{Instructions: "Default instructions"})
	params := baseGenerateOptions()
	temp := 0.5
	params.Temperature = &temp
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if got.Temperature == nil || *got.Temperature != 0.5 {
		t.Fatalf("Temperature = %v", got.Temperature)
	}
	if got.Prompt.System != "Default instructions" {
		t.Fatalf("Prompt.System = %q", got.Prompt.System)
	}
}

func TestDefaultInstructionsMiddleware_EmptyInstructionsLeaveParamsUnchanged(t *testing.T) {
	t.Parallel()
	middleware := DefaultInstructionsMiddleware(DefaultInstructionsOptions{})
	params := baseGenerateOptions()
	got, err := middleware.TransformParams(context.Background(), "generate", params, nil)
	if err != nil {
		t.Fatalf("TransformParams() error = %v", err)
	}
	if got != params {
		t.Fatalf("expected params to be returned unchanged (same pointer)")
	}
}

func TestDefaultInstructionsMiddleware_AppliesToGenerateAndStreamWithoutMutatingInput(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{}
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{
		DefaultInstructionsMiddleware(DefaultInstructionsOptions{Instructions: "Default instructions"}),
	}, nil, nil)

	params := baseGenerateOptions()
	if _, err := wrapped.DoGenerate(context.Background(), params); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if _, err := wrapped.DoStream(context.Background(), params); err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}

	if len(model.GenerateCalls) != 1 || model.GenerateCalls[0].Prompt.System != "Default instructions" {
		t.Fatalf("generate call = %+v", model.GenerateCalls)
	}
	if len(model.StreamCalls) != 1 || model.StreamCalls[0].Prompt.System != "Default instructions" {
		t.Fatalf("stream call = %+v", model.StreamCalls)
	}
	// The input params object itself must not have been mutated.
	if params.Prompt.System != "" {
		t.Fatalf("input params were mutated: Prompt.System = %q", params.Prompt.System)
	}
}

// See default_instructions_generate_test.go (package middleware_test) for
// the multi-step-generation and trusted-history tests, which need to import
// pkg/ai (pkg/ai already imports pkg/middleware, so those tests must live in
// an external test package to avoid an import cycle).

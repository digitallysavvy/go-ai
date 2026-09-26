package middleware_test

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// External test package (middleware_test) because these tests need
// ai.GenerateText, and pkg/ai already imports pkg/middleware
// (pkg/ai/middleware_alias.go) — importing pkg/ai from an internal
// `package middleware` test file would create an import cycle.
//
// Ported from middleware/default-instructions-middleware.test.ts's "wrapped
// model" describe block (ai@7.0.113).

func TestDefaultInstructionsMiddleware_AppliesOnceToEveryStepOfMultiStepGeneration(t *testing.T) {
	t.Parallel()
	callCount := 0
	tool := types.Tool{
		Name:       "weather",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			systemCount := 0
			for _, m := range opts.Prompt.Messages {
				if m.Role == types.RoleSystem {
					systemCount++
				}
			}
			if systemCount != 0 || opts.Prompt.System != "Default instructions" {
				t.Errorf("step %d: expected exactly the default instructions as system, got System=%q messages=%+v", callCount, opts.Prompt.System, opts.Prompt.Messages)
			}
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{Text: "Done.", FinishReason: types.FinishReasonStop}, nil
		},
	}
	wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{
		middleware.DefaultInstructionsMiddleware(middleware.DefaultInstructionsOptions{Instructions: "Default instructions"}),
	}, nil, nil)

	maxSteps := 2
	_, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:    wrapped,
		Prompt:   "What is the weather?",
		Tools:    []types.Tool{tool},
		StopWhen: []ai.StopCondition{ai.StepCountIs(maxSteps)},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
}

func TestDefaultInstructionsMiddleware_TrustedSystemMessageInHistoryOverridesDefaults(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if len(opts.Prompt.Messages) == 0 || opts.Prompt.Messages[0].Role != types.RoleSystem {
				t.Errorf("expected the conversation's own system message first, got %+v", opts.Prompt.Messages)
			}
			return &types.GenerateResult{Text: "Done.", FinishReason: types.FinishReasonStop}, nil
		},
	}
	wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{
		middleware.DefaultInstructionsMiddleware(middleware.DefaultInstructionsOptions{Instructions: "Default instructions"}),
	}, nil, nil)

	_, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: wrapped,
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Conversation instructions"}}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		},
		AllowSystemInMessages: true,
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

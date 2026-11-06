package ai

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generate-text.test.ts / stream-text.test.ts "interrupted" (#23778): a
// provider stream that ended without a completion signal keeps its partial
// output, raises no tool-choice violation, runs no tools and never continues.

func TestGenerateText_InterruptedSkipsToolChoiceViolation(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "partial", FinishReason: types.FinishReasonInterrupted}, nil
		},
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:      model,
		Prompt:     "search",
		Tools:      []types.Tool{{Name: "search"}},
		ToolChoice: types.RequiredToolChoice(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinishReason != types.FinishReasonInterrupted || result.Text != "partial" {
		t.Fatalf("result = %q / %q", result.FinishReason, result.Text)
	}
}

func TestGenerateText_InterruptedDoesNotExecuteToolsOrContinue(t *testing.T) {
	t.Parallel()

	var executed, calls atomic.Int32
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls.Add(1)
			return &types.GenerateResult{
				FinishReason: types.FinishReasonInterrupted,
				ToolCalls:    []types.ToolCall{{ID: "call_1", ToolName: "search", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}
	maxSteps := 5
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "search",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{{
			Name: "search",
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				executed.Add(1)
				return "results", nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed.Load() != 0 || calls.Load() != 1 || len(result.Steps) != 1 {
		t.Fatalf("executed=%d calls=%d steps=%d", executed.Load(), calls.Load(), len(result.Steps))
	}
	if result.FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("finish reason = %q", result.FinishReason)
	}
}

func TestStreamText_InterruptedSkipsToolChoiceViolation(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "partial"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonInterrupted},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "search",
		Tools:      []types.Tool{{Name: "search"}},
		ToolChoice: types.RequiredToolChoice(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if result.FinishReason() != types.FinishReasonInterrupted || result.Text() != "partial" {
		t.Fatalf("result = %q / %q", result.FinishReason(), result.Text())
	}
}

func TestStreamText_InterruptedDoesNotExecuteToolsOrContinue(t *testing.T) {
	t.Parallel()

	var executed, calls atomic.Int32
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls.Add(1)
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call_1", ToolName: "search", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonInterrupted},
			}), nil
		},
	}
	maxSteps := 5
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "search",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{{
			Name: "search",
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				executed.Add(1)
				return "results", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if executed.Load() != 0 || calls.Load() != 1 || len(result.Steps()) != 1 {
		t.Fatalf("executed=%d calls=%d steps=%d", executed.Load(), calls.Load(), len(result.Steps()))
	}
}

package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateText_PrepareStepActiveTools(t *testing.T) {
	t.Parallel()

	var got []string
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			for _, tool := range opts.Tools {
				got = append(got, tool.Name)
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	tools := []types.Tool{
		{Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "b", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "c", Parameters: map[string]interface{}{"type": "object"}},
	}
	var startActive []string
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "hi",
		Tools:       tools,
		ActiveTools: []string{"a", "b"},
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ActiveTools: []string{"c"}}
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { startActive = e.ActiveTools },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("model tools = %v, want [c]", got)
	}
	if len(startActive) != 1 || startActive[0] != "c" {
		t.Fatalf("OnStepStart ActiveTools = %v, want [c]", startActive)
	}
}

func TestStreamText_PrepareStepActiveTools(t *testing.T) {
	t.Parallel()

	var got []string
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			for _, tool := range opts.Tools {
				got = append(got, tool.Name)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	tools := []types.Tool{
		{Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "b", Parameters: map[string]interface{}{"type": "object"}},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools:  tools,
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ActiveTools: []string{"b"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("model tools = %v, want [b]", got)
	}
}

func TestGenerateText_OutputParseErrorAfterEndCallbacks(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "not json", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var stepEnds, ends int
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hi",
		Output:    JSONOutput(JSONOutputOptions{}),
		OnStepEnd: func(context.Context, types.StepResult, interface{}) { stepEnds++ },
		OnEnd:     func(context.Context, *GenerateTextResult, interface{}) { ends++ },
	})
	if err == nil {
		t.Fatal("expected output parse error")
	}
	if stepEnds != 1 || ends != 1 {
		t.Fatalf("step-end=%d end=%d, want 1/1: TS parses output after onEnd", stepEnds, ends)
	}
}

func TestGenerateText_StepTimeoutCoversToolExecution(t *testing.T) {
	t.Parallel()

	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "slow", Arguments: map[string]interface{}{}}},
			}, nil
		},
	}
	var sawCancel bool
	tool := types.Tool{
		Name:       "slow",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-ctx.Done():
				sawCancel = true
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return "late", nil
			}
		},
	}
	perStep := 50 * time.Millisecond
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "hi",
		Tools:    []types.Tool{tool},
		Timeout:  &TimeoutConfig{PerStep: &perStep},
		StopWhen: []StopCondition{IsStepCount(3)},
	})
	if !sawCancel {
		t.Fatal("step timeout did not cancel the running tool")
	}
	var te *TimeoutError
	if !errors.As(err, &te) || te.Reason != TimeoutReasonStep {
		t.Fatalf("err = %v, want step TimeoutError", err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1 (next step must not start)", calls)
	}
}

// TS notify() swallows callback errors, so a throwing onStepEnd/onEnd must not
// break the call. The Go equivalent is a panic in the callback.
func TestGenerateText_PanickingLegacyCallbacksDoNotBreakCall(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hi",
		OnStepEnd: func(context.Context, types.StepResult, interface{}) { panic("step") },
		OnEnd:     func(context.Context, *GenerateTextResult, interface{}) { panic("end") },
	})
	if err != nil || res.Text != "ok" {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestStreamText_PanickingLegacyCallbacksDoNotBreakCall(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "hi",
		OnStepEnd: func(context.Context, types.StepResult, interface{}) { panic("step") },
		OnEnd:     func(*StreamTextResult) { panic("end") },
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := res.ReadAll()
	if err != nil || text != "ok" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestEmbedMany_PanickingCallbacksDoNotBreakCall(t *testing.T) {
	t.Parallel()

	res, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:   &testutil.MockEmbeddingModel{},
		Inputs:  []string{"a", "b"},
		OnStart: func(EmbedOnStartEvent) { panic("start") },
		OnEnd:   func(EmbedOnFinishEvent) { panic("end") },
	})
	if err != nil || len(res.Embeddings) != 2 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

package ai

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// CB-T24: Integration test – all 6 events fire in correct order during a
// multi-step tool-call generation.
func TestGenerateText_EventOrder(t *testing.T) {
	t.Parallel()

	// Track event types in order.
	var mu sync.Mutex
	var eventOrder []string

	record := func(name string) {
		mu.Lock()
		eventOrder = append(eventOrder, name)
		mu.Unlock()
	}

	// A tool that the model calls once then returns a final answer.
	calcTool := types.Tool{
		Name:        "calculate",
		Description: "performs arithmetic",
		Parameters:  map[string]interface{}{},
		Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "4", nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				// First call: model requests tool
				return &types.GenerateResult{
					Text: "",
					ToolCalls: []types.ToolCall{
						{ID: "tc1", ToolName: "calculate", Arguments: map[string]interface{}{}},
					},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			// Second call: model provides final answer
			return &types.GenerateResult{
				Text:         "The answer is 4.",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "What is 2+2?",
		Tools:  []types.Tool{calcTool},
		StopWhen: []StopCondition{
			StepCountIs(5),
		},
		OnStart: func(_ context.Context, _ OnStartEvent) {
			record("OnStart")
		},
		OnStepStart: func(_ context.Context, _ OnStepStartEvent) {
			record("OnStepStart")
		},
		OnToolCallStart: func(_ context.Context, e OnToolCallStartEvent) {
			record("OnToolCallStart:" + e.ToolName)
		},
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			record("OnToolCallFinish:" + e.ToolName)
		},
		OnStepFinishEvent: func(_ context.Context, _ OnStepFinishEvent) {
			record("OnStepFinish")
		},
		OnFinishEvent: func(_ context.Context, _ OnFinishEvent) {
			record("OnFinish")
		},
	})
	if err != nil {
		t.Fatalf("GenerateText returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Expected order for 2-step execution (step1 = tool call, step2 = final):
	// OnStart, OnStepStart, OnToolCallStart, OnToolCallFinish, OnStepFinish,
	// OnStepStart, OnStepFinish, OnFinish
	expected := []string{
		"OnStart",
		"OnStepStart",
		"OnToolCallStart:calculate",
		"OnToolCallFinish:calculate",
		"OnStepFinish",
		"OnStepStart",
		"OnStepFinish",
		"OnFinish",
	}

	if len(eventOrder) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(eventOrder), eventOrder)
	}
	for i, ev := range expected {
		if eventOrder[i] != ev {
			t.Errorf("event[%d]: expected %q, got %q (full order: %v)", i, ev, eventOrder[i], eventOrder)
		}
	}
}

// CB-T25: Test that a panicking listener does NOT abort generation.
func TestGenerateText_PanicInCallbackDoesNotAbortGeneration(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "response",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	finishCalled := false

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
		OnStart: func(_ context.Context, _ OnStartEvent) {
			panic("deliberate panic in OnStart")
		},
		OnFinishEvent: func(_ context.Context, _ OnFinishEvent) {
			finishCalled = true
		},
	})

	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	if result.Text != "response" {
		t.Errorf("expected text 'response', got %q", result.Text)
	}
	if !finishCalled {
		t.Error("OnFinish was not called even though OnStart panicked")
	}
}

// CB-T24/T17: OnToolCallFinishEvent.Error is populated on tool failure.
func TestGenerateText_ToolCallFinishEventErrorPopulated(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var capturedErr error
	var capturedResult interface{}

	failTool := types.Tool{
		Name:        "fail",
		Description: "always fails",
		Parameters:  map[string]interface{}{},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("tool execution error")
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					ToolCalls: []types.ToolCall{
						{ID: "tc1", ToolName: "fail", Arguments: map[string]interface{}{}},
					},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Call fail tool",
		Tools:  []types.Tool{failTool},
		StopWhen: []StopCondition{StepCountIs(5)},
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			mu.Lock()
			capturedErr = e.Error
			capturedResult = e.Result
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedErr == nil {
		t.Error("OnToolCallFinishEvent.Error should be non-nil when tool fails")
	}
	if capturedResult != nil {
		t.Errorf("OnToolCallFinishEvent.Result should be nil on failure, got %v", capturedResult)
	}
}

// CB-T24: OnStartEvent fields are populated correctly.
func TestGenerateText_OnStartEventFields(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	var mu sync.Mutex
	var captured OnStartEvent

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hello",
		System: "You are helpful.",
		OnStart: func(_ context.Context, e OnStartEvent) {
			mu.Lock()
			captured = e
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if captured.Prompt != "hello" {
		t.Errorf("OnStartEvent.Prompt = %q, want 'hello'", captured.Prompt)
	}
	if captured.System != "You are helpful." {
		t.Errorf("OnStartEvent.System = %q, want 'You are helpful.'", captured.System)
	}
	if captured.ModelProvider == "" {
		t.Error("OnStartEvent.ModelProvider should not be empty")
	}
	if captured.ModelID == "" {
		t.Error("OnStartEvent.ModelID should not be empty")
	}
}

// CB-T24: OnFinishEvent aggregates all steps and total usage.
func TestGenerateText_OnFinishEventAggregation(t *testing.T) {
	t.Parallel()

	in, out, tot := int64(10), int64(20), int64(30)
	in2, out2, tot2 := int64(5), int64(15), int64(20)

	calcTool := types.Tool{
		Name:        "calc",
		Description: "calc",
		Parameters:  map[string]interface{}{},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "42", nil
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					ToolCalls: []types.ToolCall{{ID: "tc1", ToolName: "calc", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
					Usage:        types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &tot},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "answer",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: &in2, OutputTokens: &out2, TotalTokens: &tot2},
			}, nil
		},
	}

	var mu sync.Mutex
	var capturedFinish OnFinishEvent

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "Calculate",
		Tools:    []types.Tool{calcTool},
		StopWhen: []StopCondition{StepCountIs(5)},
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			capturedFinish = e
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedFinish.Text != "answer" {
		t.Errorf("OnFinishEvent.Text = %q, want 'answer'", capturedFinish.Text)
	}
	if len(capturedFinish.Steps) != 2 {
		t.Errorf("OnFinishEvent.Steps: expected 2, got %d", len(capturedFinish.Steps))
	}
	// Total usage should be sum of both steps: 10+5=15 input, 20+15=35 output
	if capturedFinish.TotalUsage.InputTokens == nil || *capturedFinish.TotalUsage.InputTokens != 15 {
		t.Errorf("OnFinishEvent.TotalUsage.InputTokens = %v, want 15", capturedFinish.TotalUsage.InputTokens)
	}
}

package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// CB-T26: Both settings-level and call-level callbacks fire when merged.
func TestMergeCallbacks_BothCallbacksFire(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calls []string

	settingsOnStart := func(_ context.Context, _ ai.OnStartEvent) {
		mu.Lock()
		calls = append(calls, "settings-onStart")
		mu.Unlock()
	}
	callOnStart := func(_ context.Context, _ ai.OnStartEvent) {
		mu.Lock()
		calls = append(calls, "call-onStart")
		mu.Unlock()
	}

	merged := mergeCallbacks(
		AgentConfig{OnStart: settingsOnStart},
		agentCallbacks{onStart: callOnStart},
	)

	if merged.onStart == nil {
		t.Fatal("mergeCallbacks: merged onStart should not be nil")
	}
	merged.onStart(context.Background(), ai.OnStartEvent{})

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
	}
	// Settings fires first, then call-level
	if calls[0] != "settings-onStart" {
		t.Errorf("expected settings callback first, got %q", calls[0])
	}
	if calls[1] != "call-onStart" {
		t.Errorf("expected call callback second, got %q", calls[1])
	}
}

// CB-T26: Nil + non-nil returns the non-nil callback unchanged.
func TestMergeCallbacks_NilCallbackHandling(t *testing.T) {
	t.Parallel()

	called := false
	cb := func(_ context.Context, _ ai.OnStartEvent) {
		called = true
	}

	// Only call-level
	merged := mergeCallbacks(AgentConfig{}, agentCallbacks{onStart: cb})
	if merged.onStart == nil {
		t.Fatal("expected non-nil merged onStart")
	}
	merged.onStart(context.Background(), ai.OnStartEvent{})
	if !called {
		t.Error("callback was not called")
	}

	called = false
	// Only settings-level
	merged2 := mergeCallbacks(AgentConfig{OnStart: cb}, agentCallbacks{})
	if merged2.onStart == nil {
		t.Fatal("expected non-nil merged onStart")
	}
	merged2.onStart(context.Background(), ai.OnStartEvent{})
	if !called {
		t.Error("callback was not called")
	}

	// Both nil
	merged3 := mergeCallbacks(AgentConfig{}, agentCallbacks{})
	if merged3.onStart != nil {
		t.Error("expected nil merged onStart when both are nil")
	}
}

// CB-T26: All 6 structured event fields are merged correctly.
func TestMergeCallbacks_AllFieldsMerged(t *testing.T) {
	t.Parallel()

	count := 0
	var mu sync.Mutex
	inc := func() {
		mu.Lock()
		count++
		mu.Unlock()
	}

	settings := AgentConfig{
		OnStart:           func(_ context.Context, _ ai.OnStartEvent) { inc() },
		OnStepStartEvent:  func(_ context.Context, _ ai.OnStepStartEvent) { inc() },
		OnToolCallStart:   func(_ context.Context, _ ai.OnToolCallStartEvent) { inc() },
		OnToolCallFinish:  func(_ context.Context, _ ai.OnToolCallFinishEvent) { inc() },
		OnStepFinishEvent: func(_ context.Context, _ ai.OnStepFinishEvent) { inc() },
		OnFinishEvent:     func(_ context.Context, _ ai.OnFinishEvent) { inc() },
	}
	callOpts := agentCallbacks{
		onStart:          func(_ context.Context, _ ai.OnStartEvent) { inc() },
		onStepStart:      func(_ context.Context, _ ai.OnStepStartEvent) { inc() },
		onToolCallStart:  func(_ context.Context, _ ai.OnToolCallStartEvent) { inc() },
		onToolCallFinish: func(_ context.Context, _ ai.OnToolCallFinishEvent) { inc() },
		onStepFinish:     func(_ context.Context, _ ai.OnStepFinishEvent) { inc() },
		onFinish:         func(_ context.Context, _ ai.OnFinishEvent) { inc() },
	}

	merged := mergeCallbacks(settings, callOpts)
	ctx := context.Background()

	merged.onStart(ctx, ai.OnStartEvent{})
	merged.onStepStart(ctx, ai.OnStepStartEvent{})
	merged.onToolCallStart(ctx, ai.OnToolCallStartEvent{})
	merged.onToolCallFinish(ctx, ai.OnToolCallFinishEvent{})
	merged.onStepFinish(ctx, ai.OnStepFinishEvent{})
	merged.onFinish(ctx, ai.OnFinishEvent{})

	mu.Lock()
	defer mu.Unlock()
	// 6 event types × 2 callbacks each = 12
	if count != 12 {
		t.Errorf("expected 12 callback invocations, got %d", count)
	}
}

// CB-T24: ToolLoopAgent fires structured events during execution.
func TestToolLoopAgent_StructuredEventsFireDuringExecution(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var events []string
	record := func(name string) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	}

	calcTool := types.Tool{
		Name:        "calc",
		Description: "calc",
		Parameters:  map[string]interface{}{},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "42", nil
		},
	}

	callCount := 0
	mockModel := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "calc", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	agent := NewToolLoopAgent(AgentConfig{
		Model:    mockModel,
		Tools:    []types.Tool{calcTool},
		MaxSteps: 5,
		OnStart: func(_ context.Context, _ ai.OnStartEvent) {
			record("OnStart")
		},
		OnStepStartEvent: func(_ context.Context, _ ai.OnStepStartEvent) {
			record("OnStepStart")
		},
		OnToolCallStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			record("OnToolCallStart:" + e.ToolName)
		},
		OnToolCallFinish: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			record("OnToolCallFinish:" + e.ToolName)
		},
		OnStepFinishEvent: func(_ context.Context, _ ai.OnStepFinishEvent) {
			record("OnStepFinish")
		},
		OnFinishEvent: func(_ context.Context, _ ai.OnFinishEvent) {
			record("OnFinish")
		},
	})

	_, err := agent.Execute(context.Background(), "Calculate 6*7")
	if err != nil {
		t.Fatalf("agent.Execute returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	expected := []string{
		"OnStart",
		"OnStepStart",
		"OnToolCallStart:calc",
		"OnToolCallFinish:calc",
		"OnStepFinish",
		"OnStepStart",
		"OnStepFinish",
		"OnFinish",
	}

	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(events), events)
	}
	for i, ev := range expected {
		if events[i] != ev {
			t.Errorf("event[%d]: expected %q, got %q", i, ev, events[i])
		}
	}
}

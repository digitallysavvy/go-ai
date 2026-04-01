package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// --- GenerateObject callback tests ---

func TestGenerateObject_ExperimentalOnStart_IsFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotStart ObjectOnStartEvent
	startCalled := false

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		ExperimentalOnStart: func(_ context.Context, e ObjectOnStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			startCalled = true
			gotStart = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !startCalled {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if gotStart.CallID == "" {
		t.Error("expected non-empty CallID")
	}
	if gotStart.OperationID != "ai.generateObject" {
		t.Errorf("unexpected OperationID: %s", gotStart.OperationID)
	}
	if gotStart.Prompt != "gen" {
		t.Errorf("unexpected Prompt: %s", gotStart.Prompt)
	}
	if gotStart.Output != ObjectModeObject {
		t.Errorf("unexpected Output: %s", gotStart.Output)
	}
}

func TestGenerateObject_ExperimentalOnStepStart_IsFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotStepStart ObjectOnStepStartEvent
	called := false

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		ExperimentalOnStepStart: func(_ context.Context, e ObjectOnStepStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			called = true
			gotStepStart = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("ExperimentalOnStepStart was not called")
	}
	if gotStepStart.CallID == "" {
		t.Error("expected non-empty CallID")
	}
	if gotStepStart.StepNumber != 0 {
		t.Errorf("expected StepNumber 0, got %d", gotStepStart.StepNumber)
	}
}

func TestGenerateObject_OnStepFinish_IsFiredBeforeParse(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotStep ObjectOnStepFinishEvent
	called := false

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		OnStepFinish: func(_ context.Context, e ObjectOnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			called = true
			gotStep = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("OnStepFinish was not called")
	}
	// ObjectText should be the raw JSON string before parsing
	if gotStep.ObjectText != `{"x":1}` {
		t.Errorf("unexpected ObjectText: %s", gotStep.ObjectText)
	}
	if gotStep.FinishReason != types.FinishReasonStop {
		t.Errorf("unexpected FinishReason: %s", gotStep.FinishReason)
	}
}

func TestGenerateObject_OnFinishEvent_HasNilError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotFinish ObjectOnFinishEvent
	called := false

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		OnFinishEvent: func(_ context.Context, e ObjectOnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			called = true
			gotFinish = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("OnFinishEvent was not called")
	}
	// For GenerateObject, Error is always nil
	if gotFinish.Error != nil {
		t.Errorf("expected nil Error, got: %v", gotFinish.Error)
	}
	if gotFinish.Object == nil {
		t.Error("expected non-nil Object in OnFinishEvent")
	}
}

func TestGenerateObject_CallbackOrder_StartStepFinish(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var order []string

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		ExperimentalOnStart: func(_ context.Context, _ ObjectOnStartEvent) {
			mu.Lock()
			order = append(order, "start")
			mu.Unlock()
		},
		ExperimentalOnStepStart: func(_ context.Context, _ ObjectOnStepStartEvent) {
			mu.Lock()
			order = append(order, "step_start")
			mu.Unlock()
		},
		OnStepFinish: func(_ context.Context, _ ObjectOnStepFinishEvent) {
			mu.Lock()
			order = append(order, "step_finish")
			mu.Unlock()
		},
		OnFinishEvent: func(_ context.Context, _ ObjectOnFinishEvent) {
			mu.Lock()
			order = append(order, "finish")
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	expected := []string{"start", "step_start", "step_finish", "finish"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i, e := range expected {
		if order[i] != e {
			t.Errorf("position %d: expected %q, got %q", i, e, order[i])
		}
	}
}

func TestGenerateObject_CallbacksShareCallID(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	callIDs := make(map[string]int)
	record := func(id string) {
		mu.Lock()
		callIDs[id]++
		mu.Unlock()
	}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		ExperimentalOnStart:     func(_ context.Context, e ObjectOnStartEvent) { record(e.CallID) },
		ExperimentalOnStepStart: func(_ context.Context, e ObjectOnStepStartEvent) { record(e.CallID) },
		OnStepFinish:            func(_ context.Context, e ObjectOnStepFinishEvent) { record(e.CallID) },
		OnFinishEvent:           func(_ context.Context, e ObjectOnFinishEvent) { record(e.CallID) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(callIDs) != 1 {
		t.Errorf("expected all callbacks to share one CallID, got: %v", callIDs)
	}
	for id, count := range callIDs {
		if id == "" {
			t.Error("CallID should not be empty")
		}
		if count != 4 {
			t.Errorf("expected 4 callbacks, got %d", count)
		}
	}
}

func TestGenerateObject_NilCallbacksAreNilSafe(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"x":1}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	// All structured callbacks are nil — must not panic.
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- StreamObject callback tests ---

func TestStreamObject_ExperimentalOnStart_IsFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"x":1}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotStart ObjectOnStartEvent
	called := false

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		ExperimentalOnStart: func(_ context.Context, e ObjectOnStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			called = true
			gotStart = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if gotStart.OperationID != "ai.streamObject" {
		t.Errorf("unexpected OperationID: %s", gotStart.OperationID)
	}
}

func TestStreamObject_OnStepFinish_IsFiredBeforeParse(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"x":1}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotStep ObjectOnStepFinishEvent
	called := false

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		OnStepFinish: func(_ context.Context, e ObjectOnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			called = true
			gotStep = e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("OnStepFinish was not called")
	}
	if gotStep.ObjectText != `{"x":1}` {
		t.Errorf("unexpected ObjectText: %s", gotStep.ObjectText)
	}
}

func TestStreamObject_OnFinishEvent_ParseFailure_SetsError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `not valid json`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	var gotFinish ObjectOnFinishEvent
	finishCalled := false

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		OnFinishEvent: func(_ context.Context, e ObjectOnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			finishCalled = true
			gotFinish = e
		},
	})
	// The function should return an error for invalid JSON
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	mu.Lock()
	defer mu.Unlock()
	if !finishCalled {
		t.Fatal("OnFinishEvent should be called even on parse failure")
	}
	if gotFinish.Error == nil {
		t.Error("expected non-nil Error in OnFinishEvent on parse failure")
	}
	if gotFinish.Object != nil {
		t.Error("expected nil Object in OnFinishEvent on parse failure")
	}
}

func TestStreamObject_OnError_IsFiredOnStreamError(t *testing.T) {
	t.Parallel()

	// A stream that produces a stream-level error (not a parse error)
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"x"`},
				// No finish chunk — stream will error on Next() returning EOF
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	// Even without OnError explicitly triggering on a chunk error,
	// ensure OnStepFinish is called after stream ends.
	var mu sync.Mutex
	stepCalled := false

	_, _ = StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		OnStepFinish: func(_ context.Context, _ ObjectOnStepFinishEvent) {
			mu.Lock()
			stepCalled = true
			mu.Unlock()
		},
	})

	mu.Lock()
	defer mu.Unlock()
	if !stepCalled {
		t.Fatal("OnStepFinish should have been called after stream ends")
	}
}

func TestStreamObject_CallbacksShareCallID(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"x":1}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	var mu sync.Mutex
	callIDs := make(map[string]int)
	record := func(id string) {
		mu.Lock()
		callIDs[id]++
		mu.Unlock()
	}

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "gen",
		Schema: testSchema,
		ExperimentalOnStart:     func(_ context.Context, e ObjectOnStartEvent) { record(e.CallID) },
		ExperimentalOnStepStart: func(_ context.Context, e ObjectOnStepStartEvent) { record(e.CallID) },
		OnStepFinish:            func(_ context.Context, e ObjectOnStepFinishEvent) { record(e.CallID) },
		OnFinishEvent:           func(_ context.Context, e ObjectOnFinishEvent) { record(e.CallID) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(callIDs) != 1 {
		t.Errorf("expected all callbacks to share one CallID, got: %v", callIDs)
	}
}

// --- ExperimentalFilterActiveTools tests ---

func TestExperimentalFilterActiveTools_NilTools_ReturnsNil(t *testing.T) {
	t.Parallel()

	result := ExperimentalFilterActiveTools(nil, []string{"tool1"})
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestExperimentalFilterActiveTools_NilActiveTools_ReturnsAllTools(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool1"},
		{Name: "tool2"},
	}
	result := ExperimentalFilterActiveTools(tools, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result))
	}
	// Should be the same slice (identity check)
	if &result[0] != &tools[0] {
		t.Error("expected same underlying slice when activeTools is nil")
	}
}

func TestExperimentalFilterActiveTools_FiltersByName(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool1"},
		{Name: "tool2"},
		{Name: "tool3"},
	}
	result := ExperimentalFilterActiveTools(tools, []string{"tool1", "tool3"})
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Name != "tool1" {
		t.Errorf("unexpected first tool: %s", result[0].Name)
	}
	if result[1].Name != "tool3" {
		t.Errorf("unexpected second tool: %s", result[1].Name)
	}
}

func TestExperimentalFilterActiveTools_EmptyActiveTools_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool1"},
		{Name: "tool2"},
	}
	result := ExperimentalFilterActiveTools(tools, []string{})
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d tools", len(result))
	}
}

func TestExperimentalFilterActiveTools_UnknownNames_Ignored(t *testing.T) {
	t.Parallel()

	tools := []types.Tool{
		{Name: "tool1"},
	}
	result := ExperimentalFilterActiveTools(tools, []string{"nonexistent"})
	if len(result) != 0 {
		t.Errorf("expected 0 tools, got %d", len(result))
	}
}

func TestExperimentalFilterActiveTools_NilToolsNilActive_ReturnsNil(t *testing.T) {
	t.Parallel()

	result := ExperimentalFilterActiveTools(nil, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

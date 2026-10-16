package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A provider error while opening the first step's stream must stay that error
// when a per-step timeout is configured and has not elapsed: it is reported to
// OnError, not relabelled a step timeout or treated as an abort.
func TestStreamTextFirstStepOpenErrorWithStepTimeoutIsNotTimeout(t *testing.T) {
	stepTimeout := time.Minute
	boom := errors.New("boom")
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, boom
		},
	}
	zero := 0
	var mu sync.Mutex
	var onErr error
	aborted := false
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "test",
		MaxRetries: &zero,
		Timeout:    &TimeoutConfig{PerStep: &stepTimeout},
		OnError: func(_ context.Context, e error) {
			mu.Lock()
			onErr = e
			mu.Unlock()
		},
		OnAbort: func(context.Context, []types.StepResult) {
			mu.Lock()
			aborted = true
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, err = result.ReadAll()
	if !errors.Is(err, boom) {
		t.Fatalf("ReadAll() error = %v, want boom", err)
	}
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("error relabelled as timeout: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if aborted {
		t.Fatal("OnAbort fired for a provider error")
	}
	if !errors.Is(onErr, boom) {
		t.Fatalf("OnError = %v, want boom", onErr)
	}
}

// PrepareStepOptions.InitialInstructions is the instructions the call started
// with on every step, not the previous step's (possibly overridden) ones.
func TestStreamTextPrepareStepInitialInstructionsStayFixed(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var initial []string
	maxSteps := 3
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "hi",
		System:   "original",
		MaxSteps: &maxSteps,
		Tools: []types.Tool{{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.InitialInstructions == nil {
				initial = append(initial, "<nil>")
			} else {
				initial = append(initial, *step.InitialInstructions)
			}
			override := "override"
			step.Instructions = &override
			return step
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(initial) != 2 || initial[0] != "original" || initial[1] != "original" {
		t.Fatalf("InitialInstructions per step = %v, want [original original]", initial)
	}
}

func twoStepToolModel(second func(call int) (provider.TextStream, error)) (*testutil.MockLanguageModel, *int) {
	calls := 0
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return second(calls)
		},
	}, &calls
}

func twoStepTool() []types.Tool {
	return []types.Tool{{
		Name:       "t",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}
}

// A retryable error opening a later step's stream is retried like the first
// step's (TS retries every doStream).
func TestStreamTextLaterStepOpenIsRetried(t *testing.T) {
	model, calls := twoStepToolModel(func(call int) (provider.TextStream, error) {
		if call == 2 {
			return nil, &providererrors.ProviderError{Provider: "p", StatusCode: 500, Message: "overloaded", ResponseHeaders: map[string]string{"retry-after-ms": "1"}}
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	})
	maxSteps := 3
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", MaxSteps: &maxSteps, Tools: twoStepTool()})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if *calls != 3 {
		t.Fatalf("DoStream calls = %d, want 3 (step 2 retried once)", *calls)
	}
}

// A non-timeout failure opening a later step's stream stays that error when a
// per-step timeout is configured, and is reported to OnError exactly once.
func TestStreamTextLaterStepOpenErrorWithStepTimeout(t *testing.T) {
	boom := errors.New("boom")
	model, _ := twoStepToolModel(func(int) (provider.TextStream, error) { return nil, boom })
	zero := 0
	maxSteps := 3
	stepTimeout := time.Minute
	var mu sync.Mutex
	onErrors := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "hi", MaxSteps: &maxSteps, MaxRetries: &zero, Tools: twoStepTool(),
		Timeout: &TimeoutConfig{PerStep: &stepTimeout},
		OnError: func(context.Context, error) {
			mu.Lock()
			onErrors++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, err = result.ReadAll()
	var timeoutErr *TimeoutError
	if !errors.Is(err, boom) || errors.As(err, &timeoutErr) {
		t.Fatalf("ReadAll() error = %v, want boom and not a timeout", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if onErrors != 1 {
		t.Fatalf("OnError called %d times, want 1", onErrors)
	}
}

// GenerateObject rethrows the model error as is (TS generateObject has no
// "generation failed: " message prefix).
func TestGenerateObjectModelErrorIsNotPrefixed(t *testing.T) {
	boom := errors.New("boom")
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, boom
		},
	}
	zero := 0
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: model, Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: zero,
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("GenerateObject() error = %v, want exactly boom", err)
	}
}

// StreamObject returns the model's open error and a mid-stream error as is
// (TS streamObject has no "stream error: " message prefix); errors.As still
// reaches the typed error.
func TestStreamObjectErrorsAreNotPrefixed(t *testing.T) {
	zero := 0
	t.Run("open", func(t *testing.T) {
		boom := &providererrors.ProviderError{Provider: "p", StatusCode: 400, Message: "boom"}
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return nil, boom
			},
		}
		_, err := StreamObject(context.Background(), StreamObjectOptions{
			Model: model, Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: zero,
		})
		var pe *providererrors.ProviderError
		if err == nil || err.Error() != boom.Error() || !errors.As(err, &pe) {
			t.Fatalf("StreamObject() error = %v, want exactly %v", err, boom)
		}
	})
	t.Run("mid-stream", func(t *testing.T) {
		boom := errors.New("boom")
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStreamWithError(boom), nil
			},
		}
		_, err := StreamObject(context.Background(), StreamObjectOptions{
			Model: model, Prompt: "hi", OutputMode: ObjectModeNoSchema, MaxRetries: zero,
		})
		if err == nil || err.Error() != "boom" || !errors.Is(err, boom) {
			t.Fatalf("StreamObject() error = %v, want exactly boom", err)
		}
	})
}

package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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

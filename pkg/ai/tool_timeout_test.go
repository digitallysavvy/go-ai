package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestToolTimeoutGlobalMs verifies that a tool which sleeps longer than
// TimeoutConfig.ToolMs receives a cancellation and returns an error.
func TestToolTimeoutGlobalMs(t *testing.T) {
	t.Parallel()

	toolTimeout := 50 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "sleepTool", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}

	var toolErrMu sync.Mutex
	var toolErr error

	slowTool := types.Tool{
		Name: "sleepTool",
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return "done", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{slowTool},
		StopWhen: []StopCondition{StepCountIs(1)},
		Timeout:  &TimeoutConfig{ToolMs: &toolTimeout},
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			toolErrMu.Lock()
			toolErr = e.Error
			toolErrMu.Unlock()
		},
	})

	// Either GenerateText propagates the error or OnToolCallFinish captured it.
	toolErrMu.Lock()
	capturedErr := toolErr
	toolErrMu.Unlock()

	if err == nil && capturedErr == nil {
		t.Fatal("expected a timeout error but got none")
	}
}

// TestToolTimeoutPerToolOverride verifies that a per-tool entry in Tools
// takes precedence over the global ToolMs.
func TestToolTimeoutPerToolOverride(t *testing.T) {
	t.Parallel()

	globalTimeout := 20 * time.Millisecond  // very short global timeout
	overrideTimeout := 5 * time.Second      // generous per-tool override

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "fastTool", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}

	// Tool that sleeps 100 ms — would fail under the 20ms global timeout
	// but should succeed with the 5s per-tool override.
	fastTool := types.Tool{
		Name: "fastTool",
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return "ok", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	tc := &TimeoutConfig{
		ToolMs: &globalTimeout,
		Tools:  map[string]time.Duration{"fastTool": overrideTimeout},
	}

	var toolErrMu sync.Mutex
	var toolErr error

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{fastTool},
		StopWhen: []StopCondition{StepCountIs(1)},
		Timeout:  tc,
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			toolErrMu.Lock()
			toolErr = e.Error
			toolErrMu.Unlock()
		},
	})

	toolErrMu.Lock()
	capturedErr := toolErr
	toolErrMu.Unlock()

	if err != nil || capturedErr != nil {
		t.Fatalf("expected no error with per-tool override; got err=%v toolErr=%v", err, capturedErr)
	}
}

// TestToolTimeoutNotSetNoTimeout verifies that when no ToolMs / Tools are set
// the tool executes without any artificial deadline.
func TestToolTimeoutNotSetNoTimeout(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc1", ToolName: "myTool", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}

	myTool := types.Tool{
		Name: "myTool",
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			// Verify no artificial deadline was imposed.
			if _, hasDeadline := ctx.Deadline(); hasDeadline {
				return nil, ctx.Err()
			}
			return "ok", nil
		},
	}

	var toolErrMu sync.Mutex
	var toolErr error

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{myTool},
		StopWhen: []StopCondition{StepCountIs(1)},
		// No Timeout set at all.
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			toolErrMu.Lock()
			toolErr = e.Error
			toolErrMu.Unlock()
		},
	})

	toolErrMu.Lock()
	capturedErr := toolErr
	toolErrMu.Unlock()

	if err != nil || capturedErr != nil {
		t.Fatalf("expected no error when no timeout configured; err=%v toolErr=%v", err, capturedErr)
	}
}

// TestGetToolTimeout verifies the GetToolTimeout helper directly.
func TestGetToolTimeout(t *testing.T) {
	t.Parallel()

	globalTimeout := 10 * time.Second
	overrideTimeout := 30 * time.Second

	tc := &TimeoutConfig{
		ToolMs: &globalTimeout,
		Tools:  map[string]time.Duration{"special": overrideTimeout},
	}

	// Per-tool override present
	got := tc.GetToolTimeout("special")
	if got == nil || *got != overrideTimeout {
		t.Errorf("GetToolTimeout(special) = %v, want %v", got, overrideTimeout)
	}

	// Falls back to ToolMs
	got = tc.GetToolTimeout("other")
	if got == nil || *got != globalTimeout {
		t.Errorf("GetToolTimeout(other) = %v, want %v", got, globalTimeout)
	}

	// Nil config
	var nilCfg *TimeoutConfig
	if nilCfg.GetToolTimeout("any") != nil {
		t.Error("GetToolTimeout on nil config should return nil")
	}

	// No ToolMs, no override
	emptyTC := &TimeoutConfig{}
	if emptyTC.GetToolTimeout("any") != nil {
		t.Error("GetToolTimeout on empty config should return nil")
	}
}

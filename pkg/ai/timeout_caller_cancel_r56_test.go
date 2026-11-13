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

// A caller cancellation is an abort, not a timeout, even when total and step
// timeouts are configured: TS rejects with the abort reason, while only an
// expired AbortSignal.timeout is a TimeoutError.
func TestR56CallerCancelIsNotATimeoutError(t *testing.T) {
	long := time.Hour
	timeout := &TimeoutConfig{Total: &long, PerStep: &long}

	t.Run("GenerateText model call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(c context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
				cancel()
				<-c.Done()
				return nil, c.Err()
			},
		}
		_, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "hi", Timeout: timeout})
		assertCallerCancel(t, err)
	})

	t.Run("GenerateText between steps", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				calls++
				return &types.GenerateResult{
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			},
		}
		tool := types.Tool{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				cancel() // the tool ignores the cancellation and returns a result
				return "ok", nil
			},
		}
		_, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "hi", Timeout: timeout, Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(5)}})
		assertCallerCancel(t, err)
		if calls != 1 {
			t.Fatalf("model calls = %d, want 1", calls)
		}
	})

	t.Run("StreamText stream open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(c context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
				cancel()
				<-c.Done()
				return nil, c.Err()
			},
		}
		result, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "hi", Timeout: timeout})
		if err == nil {
			_, err = result.ReadAll()
			if err == nil {
				err = result.Err()
			}
		}
		assertCallerCancel(t, err)
	})
}

func assertCallerCancel(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var te *TimeoutError
	if errors.As(err, &te) {
		t.Fatalf("err = %v is a %s TimeoutError, want a plain cancellation", err, te.Reason)
	}
}

// The SDK's own deadlines are still reported with their reason.
func TestR56SDKDeadlinesKeepTheirTimeoutReason(t *testing.T) {
	short := 20 * time.Millisecond
	long := time.Hour
	block := &testutil.MockLanguageModel{
		DoGenerateFunc: func(c context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			<-c.Done()
			return nil, c.Err()
		},
	}
	for _, tc := range []struct {
		name    string
		timeout *TimeoutConfig
		want    TimeoutReason
	}{
		{"step", &TimeoutConfig{Total: &long, PerStep: &short}, TimeoutReasonStep},
		{"total", &TimeoutConfig{Total: &short, PerStep: &long}, TimeoutReasonTotal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GenerateText(context.Background(), GenerateTextOptions{Model: block, Prompt: "hi", Timeout: tc.timeout})
			var te *TimeoutError
			if !errors.As(err, &te) || te.Reason != tc.want {
				t.Fatalf("err = %v, want a %s TimeoutError", err, tc.want)
			}
		})
	}
}

package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A PrepareCall timeout override applies to the legacy loop's steps, as it
// does to Generate (TS `timeout ?? preparedCall.timeout`).
func TestLegacyLoopUsesPrepareCallStepTimeout(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &types.GenerateResult{Text: "late", FinishReason: types.FinishReasonStop}, nil
			}
		},
	}
	step := 20 * time.Millisecond
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Timeout = &ai.TimeoutConfig{PerStep: &step}
			return c
		},
	})
	started := time.Now()
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the step deadline", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("the PrepareCall step timeout was not applied")
	}
}

// The configured per-step timeout surfaces as context.DeadlineExceeded (a
// timeout), not context.Canceled (an abort), from the model call.
func TestLegacyLoopStepTimeoutIsDeadlineExceeded(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	step := 20 * time.Millisecond
	for i := 0; i < 5; i++ {
		a := NewToolLoopAgent(AgentConfig{Model: model, Timeout: &ai.TimeoutConfig{PerStep: &step}})
		if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
	}
}

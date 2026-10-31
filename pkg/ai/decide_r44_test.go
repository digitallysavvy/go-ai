package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS decide calls abortSignal?.throwIfAborted() before each doDecide attempt,
// so a cancelled call never reaches the model.
func TestExperimentalDecide_CancelledContextNeverCallsTheModel(t *testing.T) {
	for _, maxRetries := range []int{0, 2} {
		mock := &mockDecisionModel{
			providerName: "test",
			modelID:      "test-model",
			doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
				return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		retries := maxRetries
		_, err := ExperimentalDecide(ctx, DecideOptions{
			Model:      mock,
			State:      "refund",
			Questions:  decisionQuestionsFixture(),
			MaxRetries: &retries,
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("maxRetries %d: err = %v, want context.Canceled", maxRetries, err)
		}
		if len(mock.calls) != 0 {
			t.Fatalf("maxRetries %d: the model was called %d times", maxRetries, len(mock.calls))
		}
	}
}

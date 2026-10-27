package ai

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS validateDecisionInput rejects an unknown boolean criteria key before it
// looks at any description, so the error does not depend on map order.
func TestValidateDecisionInput_BooleanCriteriaKeysCheckedBeforeDescriptions(t *testing.T) {
	questions := map[string]provider.DecisionQuestion{
		"q": {Type: "boolean", Instructions: "Is it?", Criteria: map[string]interface{}{"true": math.NaN(), "maybe": "x"}},
	}
	for i := 0; i < 50; i++ {
		err := validateDecisionInput("state", questions, nil)
		if err == nil || err.Error() != "Invalid argument for parameter questions.q: boolean criteria may only describe true and false" {
			t.Fatalf("err = %v", err)
		}
	}
}

// TS DecisionLanguageModel.doDecide throws an aborted signal before calling
// the model and again after it returns.
func TestDecisionLanguageModel_DoDecide_HonoursCancellation(t *testing.T) {
	calls := 0
	var cancel context.CancelFunc
	mock := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if cancel != nil {
				cancel()
			}
			return &types.GenerateResult{Text: `{"q0":0.25}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatal(err)
	}
	opts := provider.DecisionCallOptions{State: "x", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "?"}}}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := model.DoDecide(canceled, opts); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("pre-cancelled: err = %v, calls = %d", err, calls)
	}
	ctx, c := context.WithCancel(context.Background())
	cancel = c
	if res, err := model.DoDecide(ctx, opts); !errors.Is(err, context.Canceled) || res != nil || calls != 1 {
		t.Fatalf("cancelled during the call: res = %v, err = %v, calls = %d", res, err, calls)
	}
}

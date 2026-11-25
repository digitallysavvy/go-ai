package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func okDecisionMock() *mockDecisionModel {
	return &mockDecisionModel{
		providerName: "test", modelID: "m",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
		},
	}
}

// TS #23590: maxOutputTokens is forwarded and validated before the call.
func TestExperimentalDecide_MaxOutputTokens(t *testing.T) {
	for _, n := range []int{1, 512} {
		mock := okDecisionMock()
		v := n
		if _, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "t", Questions: decisionQuestionsFixture(), MaxOutputTokens: &v}); err != nil {
			t.Fatal(err)
		}
		if got := mock.calls[0].MaxOutputTokens; got == nil || *got != n {
			t.Fatalf("forwarded = %v", got)
		}
	}
	mock := okDecisionMock()
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "t", Questions: decisionQuestionsFixture()}); err != nil || mock.calls[0].MaxOutputTokens != nil {
		t.Fatalf("unset: %v", err)
	}
	for _, n := range []int{0, -1} {
		mock := okDecisionMock()
		v := n
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "t", Questions: decisionQuestionsFixture(), MaxOutputTokens: &v})
		if !isInvalidArgumentError(err) || len(mock.calls) != 0 {
			t.Errorf("%d: err = %v, calls = %d", n, err, len(mock.calls))
		}
	}
}

// TS #23590: reasoning is forwarded and reported in the start event.
func TestExperimentalDecide_ReasoningForwardedAndInStartEvent(t *testing.T) {
	level := types.ReasoningHigh
	mock := okDecisionMock()
	var started DecideOnStartEvent
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock, State: "t", Questions: decisionQuestionsFixture(), Reasoning: &level,
		OnStart: func(e DecideOnStartEvent) { started = e },
	}); err != nil {
		t.Fatal(err)
	}
	if got := mock.calls[0].Reasoning; got == nil || *got != level {
		t.Fatalf("forwarded = %v", got)
	}
	if started.Reasoning == nil || *started.Reasoning != level {
		t.Fatalf("start event reasoning = %v", started.Reasoning)
	}
}

// TS #23590: language-model adapters pass maxOutputTokens and default
// reasoning to none.
func TestDecisionLanguageModel_ForwardsMaxOutputTokensAndReasoning(t *testing.T) {
	var got *provider.GenerateOptions
	lm := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		got = o
		return &types.GenerateResult{Text: `{"q0":true}`, FinishReason: types.FinishReasonStop}, nil
	}}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: lm})
	if err != nil {
		t.Fatal(err)
	}
	q := map[string]provider.DecisionQuestion{"a": {Type: "boolean", Instructions: "?"}}
	_, _ = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: q})
	if got == nil || got.Reasoning == nil || *got.Reasoning != types.ReasoningNone || got.MaxTokens != nil {
		t.Fatalf("defaults: %#v", got)
	}
	n, level := 256, types.ReasoningLow
	_, _ = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: q, MaxOutputTokens: &n, Reasoning: &level})
	if *got.Reasoning != types.ReasoningLow || got.MaxTokens == nil || *got.MaxTokens != 256 {
		t.Fatalf("forwarded: %#v", got)
	}
}

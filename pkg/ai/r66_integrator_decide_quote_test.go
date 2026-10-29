package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Every decision validation message quotes the question id as a TS
// template literal does (`Question "${id}" ...`), without Go %q escaping.
func TestR66IntegratorDecisionValidationMessagesQuoteIDAsIs(t *testing.T) {
	const id = `a"b\é`
	f := func(v float64) *float64 { return &v }
	choice := provider.DecisionQuestion{Type: "choice", Instructions: "?", Criteria: map[string]interface{}{"x": "X", "y": "Y"}}
	score := provider.DecisionQuestion{Type: "score", Instructions: "?", Criteria: []interface{}{"low", "high"}}
	cases := []struct {
		name   string
		q      provider.DecisionQuestion
		answer provider.DecisionAnswer
		want   string
	}{
		{"unknown option", choice, provider.DecisionAnswer{Type: "choice", Choice: "z"}, `Question "a"b\é" selected an unknown option.`},
		{"incomplete distribution", choice, provider.DecisionAnswer{Type: "choice", Choice: "x", Probabilities: map[string]float64{"x": 1}}, `Question "a"b\é" must have a complete distribution of finite probabilities in [0, 1].`},
		{"invalid probability", choice, provider.DecisionAnswer{Type: "choice", Choice: "x", Probabilities: map[string]float64{"x": 1, "q": 0}}, `Question "a"b\é" must have a complete distribution of finite probabilities in [0, 1].`},
		{"sum", choice, provider.DecisionAnswer{Type: "choice", Choice: "x", Probabilities: map[string]float64{"x": 0.5, "y": 0.2}}, `Question "a"b\é" probabilities must sum to 1 within the declared rounding precision.`},
		{"highest", choice, provider.DecisionAnswer{Type: "choice", Choice: "x", Probabilities: map[string]float64{"x": 0.2, "y": 0.8}}, `Question "a"b\é" did not select a highest-probability option.`},
		{"score range", score, provider.DecisionAnswer{Type: "score", Score: f(5)}, `Question "a"b\é" score must be in [0, 1].`},
		{"score mean", score, provider.DecisionAnswer{Type: "score", Score: f(0), Probabilities: map[string]float64{"0": 0.2, "1": 0.8}}, `Question "a"b\é" score must equal the probability-weighted mean within the declared rounding precision.`},
		{"box", provider.DecisionQuestion{Type: "box", Instructions: "?"}, provider.DecisionAnswer{Type: "box"}, `Question "a"b\é" must return box edges in [0, 1] with left < right and top < bottom.`},
		{"boolean", provider.DecisionQuestion{Type: "boolean", Instructions: "?"}, provider.DecisionAnswer{Type: "boolean"}, `Question "a"b\é" must return P(true) as a finite probability in [0, 1].`},
	}
	for _, tc := range cases {
		err := validateDecisionAnswers(map[string]provider.DecisionQuestion{id: tc.q}, nil, map[string]provider.DecisionAnswer{id: tc.answer}, nil)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

// The decision language model's choice and score messages quote the id as
// TS decision-language-model.ts does.
func TestR66IntegratorDecisionLanguageModelMessagesQuoteIDAsIs(t *testing.T) {
	const id = `a"b\é`
	cases := []struct {
		name string
		q    provider.DecisionQuestion
		text string
		want string
	}{
		{"choice", provider.DecisionQuestion{Type: "choice", Instructions: "?", Criteria: map[string]interface{}{"x": "X"}}, `{"q0":"c9"}`, `Question "a"b\é" selected an unknown option.`},
		{"score", provider.DecisionQuestion{Type: "score", Instructions: "?", Criteria: []interface{}{"low", "high"}}, `{"q0":9}`, `Question "a"b\é" returned a score outside its rubric.`},
	}
	for _, tc := range cases {
		text := tc.text
		mock := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		}}
		model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
		if err != nil {
			t.Fatal(err)
		}
		_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "x", Questions: map[string]provider.DecisionQuestion{id: tc.q}})
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

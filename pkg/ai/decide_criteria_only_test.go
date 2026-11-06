package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #23470: instructions are optional for choice, score and boolean
// questions and the questions reach the model unchanged.
func TestExperimentalDecide_CriteriaOnlyQuestions(t *testing.T) {
	fixture := decisionQuestionsFixture()
	criteriaOnly := map[string]provider.DecisionQuestion{}
	for id, q := range fixture {
		criteriaOnly[id] = provider.DecisionQuestion{Type: q.Type, Criteria: q.Criteria}
	}
	mock := &mockDecisionModel{
		providerName: "test", modelID: "m",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
		},
	}
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "text", Questions: criteriaOnly}); err != nil {
		t.Fatal(err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("calls = %d", len(mock.calls))
	}
	for id, q := range mock.calls[0].Questions {
		if q.Instructions != nil {
			t.Errorf("%s: instructions = %v, want nil", id, q.Instructions)
		}
	}
}

func TestExperimentalDecide_SingleBooleanCriterion(t *testing.T) {
	for name, criteria := range map[string]map[string]interface{}{
		"true only":  {"true": []interface{}{"Refund requested"}},
		"false only": {"false": map[string]interface{}{"intent": "Status inquiry"}},
	} {
		prob := 0.9
		mock := &mockDecisionModel{
			providerName: "test", modelID: "m",
			doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
				return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"refund": {Type: "boolean", Probability: &prob}}}, nil
			},
		}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{
			Model: mock, State: "text",
			Questions: map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Criteria: criteria}},
		})
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExperimentalDecide_RejectsInstructionlessBooleanWithoutDescribedCriterion(t *testing.T) {
	for name, criteria := range map[string]interface{}{
		"nil":        nil,
		"empty":      map[string]interface{}{},
		"true null":  map[string]interface{}{"true": nil},
		"false null": map[string]interface{}{"false": nil},
		"both null":  map[string]interface{}{"true": nil, "false": nil},
	} {
		mock := &mockDecisionModel{providerName: "test", modelID: "m"}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{
			Model: mock, State: "text",
			Questions: map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Criteria: criteria}},
		})
		if !isInvalidArgumentError(err) {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
		if len(mock.calls) != 0 {
			t.Errorf("%s: model must not be called", name)
		}
	}
}

func TestExperimentalDecide_RejectsInvalidInstructionsAndBoxWithout(t *testing.T) {
	for name, q := range map[string]provider.DecisionQuestion{
		"box without instructions":  {Type: "box"},
		"choice number":             {Type: "choice", Instructions: 42, Criteria: map[string]interface{}{"a": nil}},
		"score bool":                {Type: "score", Instructions: false, Criteria: []interface{}{"Low", "High"}},
		"boolean func":              {Type: "boolean", Instructions: func() bool { return true }, Criteria: map[string]interface{}{"true": "Yes"}},
		"boolean criterion number":  {Type: "boolean", Criteria: map[string]interface{}{"true": 42}},
		"boolean criterion unknown": {Type: "boolean", Criteria: map[string]interface{}{"yes": "Yes"}},
	} {
		mock := &mockDecisionModel{providerName: "test", modelID: "m", supportedTypes: []string{"choice", "score", "boolean", "box"}}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "text", Questions: map[string]provider.DecisionQuestion{"q": q}})
		if !isInvalidArgumentError(err) {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
	}
}

// TS #23470: a single score level is passed through and answers are validated
// against it.
func TestExperimentalDecide_SingleLevelScore(t *testing.T) {
	run := func(answer provider.DecisionAnswer) error {
		mock := &mockDecisionModel{
			providerName: "test", modelID: "m",
			doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
				return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"severity": answer}}, nil
			},
		}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{
			Model: mock, State: "The product is broken.",
			Questions: map[string]provider.DecisionQuestion{"severity": {Type: "score", Criteria: []interface{}{"Product unusable"}}},
		})
		return err
	}
	f := func(v float64) *float64 { return &v }
	if err := run(provider.DecisionAnswer{Type: "score", Score: f(0), Probabilities: map[string]float64{"0": 1}}); err != nil {
		t.Fatalf("valid single-level score: %v", err)
	}
	for name, answer := range map[string]provider.DecisionAnswer{
		"score 1":           {Type: "score", Score: f(1), Probabilities: map[string]float64{"0": 1}},
		"probability 0.5":   {Type: "score", Score: f(0), Probabilities: map[string]float64{"0": 0.5}},
		"extra probability": {Type: "score", Score: f(0), Probabilities: map[string]float64{"0": 0.5, "1": 0.5}},
	} {
		if err := run(answer); err == nil {
			t.Errorf("%s: expected an invalid response error", name)
		}
	}
}

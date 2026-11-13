package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// DecideOptions.QuestionOrder/CriteriaOrder reach the model unchanged.
func TestExperimentalDecide_ForwardsQuestionAndCriteriaOrder(t *testing.T) {
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "test-model",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
		},
	}
	order := []string{"topic", "severity", "refund"}
	criteria := map[string][]string{"topic": {"support", "billing"}}
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock, State: "s", Questions: decisionQuestionsFixture(),
		QuestionOrder: order, CriteriaOrder: criteria,
	}); err != nil {
		t.Fatal(err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("calls = %d", len(mock.calls))
	}
	if got := mock.calls[0]; !reflect.DeepEqual(got.QuestionOrder, order) || !reflect.DeepEqual(got.CriteriaOrder, criteria) {
		t.Fatalf("QuestionOrder=%v CriteriaOrder=%v", got.QuestionOrder, got.CriteriaOrder)
	}
}

// The language-model decision prompt lists questions and choice options in
// the given TS order (rubric q0 is the first question, c0 its first option).
func TestDecisionLanguageModel_HonorsQuestionAndCriteriaOrder(t *testing.T) {
	var prompt string
	mock := &testutil.MockLanguageModel{
		ProviderName: "test", ModelName: "test-model",
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			for _, m := range opts.Prompt.Messages {
				for _, p := range m.Content {
					if tc, ok := p.(types.TextContent); ok && strings.HasPrefix(tc.Text, `{"questions"`) {
						prompt = tc.Text
					}
				}
			}
			return &types.GenerateResult{Text: `{"q0":"c0","q1":0.5}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	dm, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatal(err)
	}
	_, err = dm.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"a": {Type: "boolean", Instructions: "A?"},
			"z": {Type: "choice", Instructions: "Z?", Criteria: map[string]interface{}{"alpha": nil, "omega": nil}},
		},
		QuestionOrder: []string{"z", "a"},
		CriteriaOrder: map[string][]string{"z": {"omega", "alpha"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"questions":{"q0":{"id":"z","type":"choice","instructions":"Z?","criteria":{"c0":{"label":"omega","description":null},"c1":{"label":"alpha","description":null}}},"q1":{"id":"a","type":"boolean","instructions":"A?"}}}`
	if prompt != want {
		t.Fatalf("prompt =\n%s\nwant\n%s", prompt, want)
	}
}

// With 11+ options the rubric lists c0..c10 in option order, as TS's
// Object.fromEntries does (a Go map would put c10 before c2).
func TestDecisionLanguageModel_RubricKeepsOptionCodeOrder(t *testing.T) {
	var prompt string
	mock := &testutil.MockLanguageModel{
		ProviderName: "test", ModelName: "test-model",
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			prompt = opts.Prompt.Messages[0].Content[0].(types.TextContent).Text
			return &types.GenerateResult{Text: `{"q0":"c0"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	dm, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatal(err)
	}
	criteria := map[string]interface{}{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		criteria[k] = nil
	}
	if _, err := dm.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: map[string]provider.DecisionQuestion{"q": {Type: "choice", Instructions: "Q?", Criteria: criteria}},
	}); err != nil {
		t.Fatal(err)
	}
	i2, i10 := strings.Index(prompt, `"c2":`), strings.Index(prompt, `"c10":`)
	if i2 < 0 || i10 < 0 || i2 > i10 {
		t.Fatalf("option codes out of order (c2 at %d, c10 at %d): %s", i2, i10, prompt)
	}
}

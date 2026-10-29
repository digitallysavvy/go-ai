package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS builds the decision error messages with template literals
// (`Question "${id}" ...`): the question id appears as is, without Go %q
// escaping of quotes, backslashes or non-ASCII characters.
func TestR66DecisionErrorsQuoteIDAsIs(t *testing.T) {
	const id = `a"b\é`
	questions := map[string]provider.DecisionQuestion{id: {Type: "boolean", Instructions: "?"}}
	err := validateDecisionAnswers(questions, nil, map[string]provider.DecisionAnswer{id: {Type: "choice"}}, nil)
	if want := `Question "a"b\é" returned an answer with the wrong type.`; err == nil || err.Error() != want {
		t.Errorf("validate: err = %v, want %q", err, want)
	}

	mock := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: `{"q0":2}`, FinishReason: types.FinishReasonStop}, nil
	}}
	model, mErr := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if mErr != nil {
		t.Fatal(mErr)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "x", Questions: questions})
	if want := `Question "a"b\é" must return P(true) as a finite probability in [0, 1].`; err == nil || err.Error() != want {
		t.Errorf("language model: err = %v, want %q", err, want)
	}

	unsupported := &providererrors.DecisionUnsupportedQuestionTypeError{QuestionID: id, QuestionType: "box", Provider: `p"`, ModelID: "m"}
	if want := `Question "a"b\é" has type "box", which is not supported by provider "p"" and model "m".`; unsupported.Error() != want {
		t.Errorf("unsupported: %q, want %q", unsupported.Error(), want)
	}
}

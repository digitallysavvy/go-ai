package ai

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ruling 4 (round 96): a generated object holding a number beyond the double
// range parses as Infinity (TS safeParseJSON -> JSON.parse) instead of
// failing with "could not parse the response".
func TestR96genObjectNonFiniteNumber(t *testing.T) {
	obj, _, _, err := parseStreamFinal(ObjectModeNoSchema, nil, nil, `{"a":1e400}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m, _ := obj.(map[string]interface{})
	if v, _ := m["a"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("object = %#v, want a = +Inf", obj)
	}
}

// Ruling 4 (round 96): TS DecisionLanguageModel reads 1e400 as Infinity, which
// fails the finite check ("must return P(true) as a finite probability"), not
// the JSON parse.
func TestR96genDecisionLanguageModelNonFiniteNumber(t *testing.T) {
	lm := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: `{"q0":1e400}`, FinishReason: types.FinishReasonStop}, nil
	}}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: lm})
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: map[string]provider.DecisionQuestion{"a": {Type: "boolean", Instructions: "?"}},
	})
	var invalid *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalid) || invalid.Message != `Question "a" must return P(true) as a finite probability in [0, 1].` {
		t.Fatalf("err = %v, want the finite probability error", err)
	}
}

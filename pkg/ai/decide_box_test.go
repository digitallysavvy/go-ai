package ai

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func boxMock(box *provider.DecisionBox) *mockDecisionModel {
	return &mockDecisionModel{
		providerName: "test", modelID: "m", supportedTypes: []string{"box"},
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"target": {Type: "box", Box: box}}}, nil
		},
	}
}

var boxQuestions = map[string]provider.DecisionQuestion{"target": {Type: "box", Instructions: "The cat"}}

// TS #23412: valid normalized box coordinates are preserved.
func TestExperimentalDecide_PreservesBox(t *testing.T) {
	want := provider.DecisionBox{Top: 0.25, Bottom: 0.82, Left: 0.44, Right: 0.84}
	res, err := ExperimentalDecide(context.Background(), DecideOptions{Model: boxMock(&want), State: "", Questions: boxQuestions})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Answers["target"]
	if got.Type != "box" || got.Box == nil || *got.Box != want {
		t.Fatalf("answer = %#v", got)
	}
}

func TestExperimentalDecide_RejectsMalformedBox(t *testing.T) {
	valid := provider.DecisionBox{Top: 0.25, Bottom: 0.82, Left: 0.44, Right: 0.84}
	with := func(f func(b *provider.DecisionBox)) *provider.DecisionBox { b := valid; f(&b); return &b }
	cases := map[string]*provider.DecisionBox{
		"missing":        nil,
		"top negative":   with(func(b *provider.DecisionBox) { b.Top = -0.1 }),
		"bottom > 1":     with(func(b *provider.DecisionBox) { b.Bottom = 1.1 }),
		"left NaN":       with(func(b *provider.DecisionBox) { b.Left = math.NaN() }),
		"right Inf":      with(func(b *provider.DecisionBox) { b.Right = math.Inf(1) }),
		"left > right":   with(func(b *provider.DecisionBox) { b.Left = 0.9 }),
		"bottom < top":   with(func(b *provider.DecisionBox) { b.Bottom = 0.2 }),
		"right == left":  with(func(b *provider.DecisionBox) { b.Right = b.Left }),
		"bottom == top":  with(func(b *provider.DecisionBox) { b.Bottom = b.Top }),
		"empty zero box": {},
	}
	for name, box := range cases {
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: boxMock(box), State: "", Questions: boxQuestions})
		var target *providererrors.InvalidResponseDataError
		if !errors.As(err, &target) {
			t.Errorf("%s: err = %v, want InvalidResponseDataError", name, err)
		}
	}
}

// TS #23412: OpenAI, Google and Gateway decision models reject box questions
// before calling the provider.
func TestExperimentalDecide_BoxUnsupportedByLanguageAndGatewayModels(t *testing.T) {
	openaiModel, err := openai.New(openai.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).DecisionModel("test")
	if err != nil {
		t.Fatal(err)
	}
	gatewayProvider, err := gateway.New(gateway.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	gatewayModel, err := gatewayProvider.DecisionModel("test")
	if err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]provider.DecisionModel{"openai": openaiModel, "gateway": gatewayModel} {
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, State: "", Questions: boxQuestions})
		var target *providererrors.DecisionUnsupportedQuestionTypeError
		if !errors.As(err, &target) {
			t.Errorf("%s: err = %v, want DecisionUnsupportedQuestionTypeError", name, err)
		}
	}
}

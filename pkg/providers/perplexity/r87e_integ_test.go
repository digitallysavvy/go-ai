package perplexity

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS perplexity-decision-model.ts throws the @ai-sdk/provider
// InvalidArgumentError, whose message has no parameter prefix.
func TestR87eIntegDecisionQuestionCountBareMessage(t *testing.T) {
	m, err := New(Config{APIKey: "k"}).DecisionModel("pplx-decider-latest")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoDecide(context.Background(), provider.DecisionCallOptions{})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) || ia.Field != "questions" {
		t.Fatalf("err = %T %v", err, err)
	}
	if got := err.Error(); got != "Perplexity Decisions supports 1 to 128 questions per request." {
		t.Fatalf("Error() = %q", got)
	}
}

// A direct DoDecide caller's typed Go criteria ([]string, map[string]string)
// send the same body as the JSON form: TS providers always receive JSON
// (cr-r87e-gen-to-integrator CR 2).
func TestR87eIntegDirectDoDecideTypedCriteria(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	model := s.model(t, "pplx-decider-latest")
	if _, err := model.DoDecide(context.Background(), decisionOptions()); err != nil {
		t.Fatal(err)
	}
	typed := decisionOptions()
	typed.Questions["sentiment"] = provider.DecisionQuestion{Type: "choice", Instructions: "What is the sentiment?", Criteria: map[string]string{
		"positive": "Satisfied", "mixed": "Praise and complaints", "negative": "Dissatisfied",
	}}
	typed.Questions["severity"] = provider.DecisionQuestion{Type: "score", Instructions: "How severe is the problem?", Criteria: []string{"Cosmetic", "Inconvenient", "Product unusable"}}
	if _, err := model.DoDecide(context.Background(), typed); err != nil {
		t.Fatal(err)
	}
	if len(s.bodies) != 2 || !reflect.DeepEqual(s.bodies[0], s.bodies[1]) {
		t.Fatalf("bodies differ:\n%v\n%v", s.bodies[0], s.bodies[1])
	}
}

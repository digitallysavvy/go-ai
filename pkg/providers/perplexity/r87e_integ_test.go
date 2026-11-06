package perplexity

import (
	"context"
	"errors"
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

package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// With several unsupported questions the reported one is deterministic (TS
// reports the first in insertion order; Go reports the first sorted ID).
func TestExperimentalDecideUnsupportedQuestionIsDeterministic(t *testing.T) {
	questions := map[string]provider.DecisionQuestion{}
	for _, id := range []string{"e", "c", "a", "d", "b"} {
		questions[id] = provider.DecisionQuestion{Type: "boolean", Instructions: "?"}
	}
	for i := 0; i < 40; i++ {
		model := &mockDecisionModel{providerName: "p", modelID: "m", supportedTypes: []string{"choice"}}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, State: "s", Questions: questions})
		var unsupported *providererrors.DecisionUnsupportedQuestionTypeError
		if !errors.As(err, &unsupported) || unsupported.QuestionID != "a" {
			t.Fatalf("iteration %d: err = %v, want unsupported question \"a\"", i, err)
		}
	}
}

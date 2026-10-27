package ai

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS validateDecisionInput rejects an unknown boolean criteria key before it
// looks at any description, so the error does not depend on map order.
func TestValidateDecisionInput_BooleanCriteriaKeysCheckedBeforeDescriptions(t *testing.T) {
	questions := map[string]provider.DecisionQuestion{
		"q": {Type: "boolean", Instructions: "Is it?", Criteria: map[string]interface{}{"true": math.NaN(), "maybe": "x"}},
	}
	for i := 0; i < 50; i++ {
		err := validateDecisionInput("state", questions, nil)
		if err == nil || err.Error() != "Invalid argument for parameter questions.q: boolean criteria may only describe true and false" {
			t.Fatalf("err = %v", err)
		}
	}
}

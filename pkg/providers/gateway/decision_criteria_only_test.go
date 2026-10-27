package gateway

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #23470: criteria-only questions are forwarded without instructions.
func TestEncodeGatewayDecisionQuestions_CriteriaOnly(t *testing.T) {
	out := encodeGatewayDecisionQuestions(map[string]provider.DecisionQuestion{
		"correct": {Type: "boolean", Criteria: map[string]interface{}{"true": "Factually correct"}},
		"quality": {Type: "score", Criteria: []interface{}{"Poor", "Great"}},
	})
	for id, wire := range out {
		if _, ok := wire.(map[string]interface{})["instructions"]; ok {
			t.Errorf("%s: instructions must be omitted: %#v", id, wire)
		}
	}
}

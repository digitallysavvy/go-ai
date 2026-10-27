package openai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #23470: a criteria-only question is sent without instructions and is not
// rejected locally.
func TestDecisionQuestionWire_CriteriaOnly(t *testing.T) {
	choice := decisionQuestionWire("department", provider.DecisionQuestion{Type: "choice", Criteria: map[string]interface{}{"billing": []interface{}{"Charges"}}})
	if _, ok := choice["instructions"]; ok {
		t.Fatalf("choice has instructions: %#v", choice)
	}
	score := decisionQuestionWire("severity", provider.DecisionQuestion{Type: "score", Criteria: []interface{}{"Low", "High"}})
	if _, ok := score["instructions"]; ok {
		t.Fatalf("score has instructions: %#v", score)
	}
	boolean := decisionQuestionWire("refund", provider.DecisionQuestion{Type: "boolean", Criteria: map[string]interface{}{
		"true": "Requests money back", "false": []interface{}{"Status inquiry"},
	}})
	want := map[string]interface{}{
		"type": "predicate", "name": "refund",
		"instructions": "Criteria for true:\nRequests money back\n\nCriteria for false:\n[\"Status inquiry\"]",
	}
	if !reflect.DeepEqual(boolean, want) {
		t.Fatalf("boolean = %#v, want %#v", boolean, want)
	}
}

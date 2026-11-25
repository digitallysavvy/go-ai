package typesafeai

import (
	"reflect"
	"testing"
)

// TS 05bf55224f catalog refactor (ruling 13): supportedQuestionTypes moved
// to typeSafeAiSupportedQuestionTypes and is the same for any model ID.
func TestPortProvcSupportedQuestionTypesForUnknownIDs(t *testing.T) {
	for _, id := range []string{"jev-latest", "jev-9.9.9", "future-model", ""} {
		got := NewDecisionModel(New(Config{APIKey: "k"}), id).SupportedQuestionTypes()
		if !reflect.DeepEqual(got, []string{"choice", "score", "boolean", "box"}) {
			t.Errorf("SupportedQuestionTypes(%q) = %v", id, got)
		}
	}
}

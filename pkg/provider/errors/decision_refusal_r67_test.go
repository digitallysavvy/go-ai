package errors

import "testing"

// TS DecisionRefusalError quotes each question ID with JSON.stringify, which
// leaves <, >, & and U+2028/U+2029 literal (json.Marshal escapes them).
func TestDecisionRefusalErrorQuotesLikeJSONStringify(t *testing.T) {
	err := &DecisionRefusalError{
		QuestionIDs: []string{"a<b>&c", "line sep", `q"x`},
		Provider:    "p",
		ModelID:     "m",
	}
	want := "Decision model \"m\" from provider \"p\" refused questions \"a<b>&c\", \"line sep\", \"q\\\"x\"."
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

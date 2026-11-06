package errors

import (
	"errors"
	"fmt"
	"testing"
)

// Ports TS decision-refusal-error.test.ts.
func TestDecisionRefusalError_Message(t *testing.T) {
	err := &DecisionRefusalError{QuestionIDs: []string{"precursor"}, Provider: "openai.decisions", ModelID: "gpt-6-luna"}
	want := `Decision model "gpt-6-luna" from provider "openai.decisions" refused question "precursor".`
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
	err.QuestionIDs = []string{"precursor", "yield"}
	want = `Decision model "gpt-6-luna" from provider "openai.decisions" refused questions "precursor", "yield".`
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func TestIsDecisionRefusalError(t *testing.T) {
	wrapped := fmt.Errorf("wrap: %w", &DecisionRefusalError{QuestionIDs: []string{"q"}})
	if !IsDecisionRefusalError(wrapped) {
		t.Fatal("expected wrapped refusal error to be recognized")
	}
	for _, e := range []error{nil, errors.New("Unrelated error")} {
		if IsDecisionRefusalError(e) {
			t.Fatalf("%v must not be recognized", e)
		}
	}
}

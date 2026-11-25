package errors

import (
	"errors"
	"fmt"
	"testing"
)

// Ports TS extraction-refusal-error.test.ts (cb4fa52341).
func TestPortCarryExtractionRefusalErrorMessage(t *testing.T) {
	err := &ExtractionRefusalError{FieldIDs: []string{"total"}, Provider: "mock-provider", ModelID: "mock-model-id"}
	want := `Extraction model "mock-model-id" from provider "mock-provider" refused field "total".`
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
	err.FieldIDs = []string{"total", "dueDate"}
	want = `Extraction model "mock-model-id" from provider "mock-provider" refused fields "total", "dueDate".`
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func TestPortCarryIsExtractionRefusalError(t *testing.T) {
	if !IsExtractionRefusalError(fmt.Errorf("wrap: %w", &ExtractionRefusalError{FieldIDs: []string{"q"}})) {
		t.Fatal("expected wrapped refusal error to be recognized")
	}
	for _, e := range []error{nil, errors.New("Unrelated error"), &DecisionRefusalError{}} {
		if IsExtractionRefusalError(e) {
			t.Fatalf("%v must not be recognized", e)
		}
	}
}

// Ports TS extraction-unsupported-field-type-error.test.ts (cb4fa52341).
func TestPortCarryExtractionUnsupportedFieldTypeErrorMessage(t *testing.T) {
	err := &ExtractionUnsupportedFieldTypeError{FieldID: "total", FieldType: "number", Provider: "mock-provider", ModelID: "mock-model-id"}
	want := `Field "total" has type "number", which is not supported by provider "mock-provider" and model "mock-model-id".`
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func TestPortCarryIsExtractionUnsupportedFieldTypeError(t *testing.T) {
	if !IsExtractionUnsupportedFieldTypeError(fmt.Errorf("wrap: %w", &ExtractionUnsupportedFieldTypeError{})) {
		t.Fatal("expected wrapped error to be recognized")
	}
	for _, e := range []error{nil, errors.New("Unrelated error"), &DecisionUnsupportedQuestionTypeError{}} {
		if IsExtractionUnsupportedFieldTypeError(e) {
			t.Fatalf("%v must not be recognized", e)
		}
	}
}

// NoSuchModelError carries the extractionModel model type (TS 10ff0b1a1d).
func TestPortCarryNoSuchExtractionModel(t *testing.T) {
	err := NewNoSuchModelError("x", "extractionModel")
	if err.Error() != "No such extractionModel: x" {
		t.Fatalf("message = %q", err.Error())
	}
}

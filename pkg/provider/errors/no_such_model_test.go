package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestNoSuchModelError_DefaultMessage(t *testing.T) {
	t.Parallel()

	err := NewNoSuchModelError("gpt-5", "languageModel")
	want := "No such languageModel: gpt-5"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestNoSuchModelError_CustomMessage(t *testing.T) {
	t.Parallel()

	err := &NoSuchModelError{ModelID: "x", ModelType: "imageModel", Message: "custom message"}
	if err.Error() != "custom message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "custom message")
	}
}

func TestIsNoSuchModelError(t *testing.T) {
	t.Parallel()

	err := NewNoSuchModelError("m", "embeddingModel")
	if !IsNoSuchModelError(err) {
		t.Error("expected IsNoSuchModelError to return true")
	}

	wrapped := fmt.Errorf("wrapped: %w", err)
	if !IsNoSuchModelError(wrapped) {
		t.Error("expected IsNoSuchModelError to unwrap and return true")
	}

	if IsNoSuchModelError(errors.New("plain error")) {
		t.Error("expected IsNoSuchModelError to return false for unrelated error")
	}
}

func TestNoSuchModelError_Fields(t *testing.T) {
	t.Parallel()

	err := NewNoSuchModelError("claude-x", "videoModel")
	if err.ModelID != "claude-x" {
		t.Errorf("ModelID = %q, want %q", err.ModelID, "claude-x")
	}
	if err.ModelType != "videoModel" {
		t.Errorf("ModelType = %q, want %q", err.ModelType, "videoModel")
	}
}

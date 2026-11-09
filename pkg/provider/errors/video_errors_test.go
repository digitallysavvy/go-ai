package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestNoVideoGeneratedError(t *testing.T) {
	err := NewNoVideoGeneratedError()
	if err.Message != "no videos were generated" {
		t.Fatalf("default message = %q", err.Message)
	}
	if err.Error() != "no videos were generated" {
		t.Fatalf("Error() = %q", err.Error())
	}

	custom := NewNoVideoGeneratedErrorWithMessage("provider returned empty outputs")
	if custom.Error() != "provider returned empty outputs" {
		t.Fatalf("custom Error() = %q", custom.Error())
	}
}

func TestVideoGenerationErrorFormattingAndUnwrap(t *testing.T) {
	cause := errors.New("upstream timeout")
	err := NewVideoGenerationError("klingai", "kling-v1", "request failed", cause)
	if err.Unwrap() != cause {
		t.Fatal("Unwrap() should return cause")
	}
	// TS provider video models throw AISDKError({message}): the text is the
	// provider message alone (r89 integrator, BFL failure text).
	msg := err.Error()
	if msg != "request failed: upstream timeout" || err.Provider != "klingai" || err.ModelID != "kling-v1" {
		t.Fatalf("Error() = %q", msg)
	}

	noCause := NewVideoGenerationError("xai", "grok-video", "request failed", nil)
	if noCause.Error() != "request failed" {
		t.Fatalf("Error() should not contain cause when nil: %q", noCause.Error())
	}
}

func TestVideoPollingTimeoutError(t *testing.T) {
	err := NewVideoPollingTimeoutError("xai", "grok-video", "job-123", "2m")
	msg := err.Error()
	if !strings.Contains(msg, "provider=xai") || !strings.Contains(msg, "model=grok-video") || !strings.Contains(msg, "job=job-123") || !strings.Contains(msg, "timeout=2m") {
		t.Fatalf("Error() = %q", msg)
	}
}

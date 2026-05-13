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
	msg := err.Error()
	if !strings.Contains(msg, "provider=klingai") || !strings.Contains(msg, "model=kling-v1") || !strings.Contains(msg, "request failed") || !strings.Contains(msg, "upstream timeout") {
		t.Fatalf("Error() = %q", msg)
	}

	noCause := NewVideoGenerationError("xai", "grok-video", "request failed", nil)
	if strings.Contains(noCause.Error(), "upstream timeout") {
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

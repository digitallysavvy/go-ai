package errors

import (
	"errors"
	"testing"
)

func TestNewStreamProviderError_MessageHeuristic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		message  string
		wantCode int
	}{
		{"Overloaded", 503},
		{"overloaded error", 503},
		{"Model Overloaded", 503},
		{"Internal Server Error", 500},
		{"Service Unavailable", 503},
	}
	for _, tt := range tests {
		e := NewStreamProviderError(tt.message, "test-provider", "", nil, nil, nil, nil)
		if e.StatusCode == nil || *e.StatusCode != tt.wantCode {
			t.Errorf("message %q: StatusCode = %v, want %d", tt.message, e.StatusCode, tt.wantCode)
		}
		if !e.IsRetryable {
			t.Errorf("message %q: IsRetryable = false, want true", tt.message)
		}
	}
}

func TestNewStreamProviderError_StatusCodeDefaultRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   bool
	}{
		{400, false},
		{408, true},
		{409, true},
		{429, true},
		{500, true},
		{503, true},
	}
	for _, tt := range tests {
		status := tt.status
		e := NewStreamProviderError("boom", "p", "", nil, &status, nil, nil)
		if e.IsRetryable != tt.want {
			t.Errorf("status %d: IsRetryable = %v, want %v", tt.status, e.IsRetryable, tt.want)
		}
	}
}

func TestNewStreamProviderError_ExplicitOverride(t *testing.T) {
	t.Parallel()

	status := 500
	falseVal := false
	e := NewStreamProviderError("boom", "p", "", nil, &status, &falseVal, nil)
	if e.IsRetryable {
		t.Fatal("expected explicit isRetryable override to win over the 500 default")
	}
}

func TestIsStreamProviderError(t *testing.T) {
	t.Parallel()

	if !IsStreamProviderError(&StreamProviderError{Message: "x"}) {
		t.Error("expected IsStreamProviderError to return true for *StreamProviderError")
	}
	if IsStreamProviderError(errors.New("plain")) {
		t.Error("expected IsStreamProviderError to return false for a plain error")
	}
}

func TestNormalizeStreamProviderError_LeavesKnownTypesUnchanged(t *testing.T) {
	t.Parallel()

	pe := &ProviderError{Provider: "p", StatusCode: 500, Message: "boom"}
	if got := NormalizeStreamProviderError(pe, "p", nil); got != error(pe) {
		t.Errorf("expected a *ProviderError to pass through unchanged, got %#v", got)
	}

	spe := &StreamProviderError{Message: "boom"}
	if got := NormalizeStreamProviderError(spe, "p", nil); got != error(spe) {
		t.Errorf("expected an existing *StreamProviderError to pass through unchanged, got %#v", got)
	}
}

func TestNormalizeStreamProviderError_WrapsPlainError(t *testing.T) {
	t.Parallel()

	raw := errors.New("Internal Server Error")
	got := NormalizeStreamProviderError(raw, "openai", nil)
	var streamErr *StreamProviderError
	if !errors.As(got, &streamErr) {
		t.Fatalf("expected a *StreamProviderError, got %T: %v", got, got)
	}
	if streamErr.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", streamErr.Provider, "openai")
	}
	if !streamErr.IsRetryable {
		t.Error("expected \"Internal Server Error\" to normalize as retryable")
	}
}

func TestNormalizeStreamProviderError_NilIsNil(t *testing.T) {
	t.Parallel()
	if got := NormalizeStreamProviderError(nil, "p", nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

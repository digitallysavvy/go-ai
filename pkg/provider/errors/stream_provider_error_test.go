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

// TestNormalizeStreamProviderError_ExtractsStructuredData ports the shape of
// TS normalize-stream-provider-error.test.ts's structured-payload cases: a
// raw provider error object (type/code/statusCode/isRetryable) attached as
// data is extracted into the normalized *StreamProviderError, mirroring TS's
// field-by-field resolution exactly.
func TestNormalizeStreamProviderError_ExtractsStructuredData(t *testing.T) {
	t.Parallel()

	raw := errors.New("Overloaded")
	data := map[string]interface{}{
		"message":     "Overloaded",
		"type":        "overloaded_error",
		"code":        "provider_overloaded",
		"statusCode":  float64(529), // Anthropic's real "overloaded" status; within TS's 400-599 range.
		"isRetryable": true,
	}
	got := NormalizeStreamProviderError(raw, "anthropic", data)
	var streamErr *StreamProviderError
	if !errors.As(got, &streamErr) {
		t.Fatalf("expected a *StreamProviderError, got %T: %v", got, got)
	}
	if streamErr.Type != "overloaded_error" {
		t.Errorf("Type = %q, want %q", streamErr.Type, "overloaded_error")
	}
	if streamErr.Code != "provider_overloaded" {
		t.Errorf("Code = %v, want %q", streamErr.Code, "provider_overloaded")
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 529 {
		t.Errorf("StatusCode = %v, want 529", streamErr.StatusCode)
	}
	if !streamErr.IsRetryable {
		t.Error("expected IsRetryable = true (explicit)")
	}
	if streamErr.Message != "Overloaded" {
		t.Errorf("Message = %q, want %q", streamErr.Message, "Overloaded")
	}
}

// TestNormalizeStreamProviderError_ExtractsNestedResponseError verifies the
// `response.error` nesting TS's normalizeStreamProviderError checks before
// falling back to a top-level `error` object or the payload itself.
func TestNormalizeStreamProviderError_ExtractsNestedResponseError(t *testing.T) {
	t.Parallel()

	raw := errors.New("fallback message")
	data := map[string]interface{}{
		"response": map[string]interface{}{
			"error": map[string]interface{}{
				"message": "nested message",
				"type":    "nested_type",
			},
		},
	}
	got := NormalizeStreamProviderError(raw, "p", data)
	var streamErr *StreamProviderError
	if !errors.As(got, &streamErr) {
		t.Fatalf("expected a *StreamProviderError, got %T: %v", got, got)
	}
	if streamErr.Message != "nested message" {
		t.Errorf("Message = %q, want %q", streamErr.Message, "nested message")
	}
	if streamErr.Type != "nested_type" {
		t.Errorf("Type = %q, want %q", streamErr.Type, "nested_type")
	}
}

// TestNormalizeStreamProviderError_SnakeCaseFields verifies the
// is_retryable/status_code snake_case variants TS also checks.
func TestNormalizeStreamProviderError_SnakeCaseFields(t *testing.T) {
	t.Parallel()

	raw := errors.New("boom")
	data := map[string]interface{}{
		"message":      "boom",
		"status_code":  float64(503),
		"is_retryable": false,
	}
	got := NormalizeStreamProviderError(raw, "p", data)
	var streamErr *StreamProviderError
	if !errors.As(got, &streamErr) {
		t.Fatalf("expected a *StreamProviderError, got %T: %v", got, got)
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 503 {
		t.Errorf("StatusCode = %v, want 503", streamErr.StatusCode)
	}
	if streamErr.IsRetryable {
		t.Error("expected explicit is_retryable=false to override the 503 default")
	}
}

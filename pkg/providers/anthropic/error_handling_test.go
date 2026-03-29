package anthropic

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestAnthropicErrorCodePreserved verifies that when the Anthropic API returns an error
// response with a "type" field (e.g. "overloaded_error"), that type is surfaced as
// ProviderError.ErrorCode rather than being discarded.
func TestAnthropicErrorCodePreserved(t *testing.T) {
	m := &LanguageModel{}

	// Simulate the error string that the internal HTTP client produces when it
	// receives a 4xx/5xx from Anthropic:
	//   "HTTP 529: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}"
	raw := errors.New(`HTTP 529: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)

	err := m.handleError(raw)
	if err == nil {
		t.Fatal("handleError returned nil")
	}

	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("handleError returned %T, want *ProviderError", err)
	}

	if provErr.ErrorCode != "overloaded_error" {
		t.Errorf("ErrorCode = %q, want %q", provErr.ErrorCode, "overloaded_error")
	}
	if provErr.Message != "Overloaded" {
		t.Errorf("Message = %q, want %q", provErr.Message, "Overloaded")
	}
	if provErr.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", provErr.Provider, "anthropic")
	}
}

// TestAnthropicErrorCodeNonJSON verifies that non-JSON errors (e.g. network errors)
// are still wrapped as ProviderErrors without crashing.
func TestAnthropicErrorCodeNonJSON(t *testing.T) {
	m := &LanguageModel{}
	raw := errors.New("connection refused")
	err := m.handleError(raw)
	if err == nil {
		t.Fatal("handleError returned nil")
	}

	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("handleError returned %T, want *ProviderError", err)
	}
	// No error code should be extracted from a non-JSON message
	if provErr.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, want empty for non-JSON errors", provErr.ErrorCode)
	}
}

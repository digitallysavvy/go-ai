package anthropic

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// P1-1c part 2: a mid-stream `error` SSE event (TS anthropic-language-model.ts
// `case 'error'`, ~line 3044) previously fell through to the "Unknown event"
// default in the Go stream and was silently discarded. Port TS's
// createAnthropicStreamError / getAnthropicStreamErrorMetadata: the chunk's
// Err field must carry a *providererrors.StreamProviderError with the
// inferred statusCode/isRetryable for the error type.

// TestStream_OverloadedErrorEvent ports TS's overloaded_error case: type
// overloaded_error infers statusCode 529 and isRetryable true when the wire
// event doesn't supply its own.
func TestStream_OverloadedErrorEvent(t *testing.T) {
	sseData := "" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("chunk.Type = %v, want ChunkTypeError", chunk.Type)
	}
	if !strings.Contains(chunk.Text, "Overloaded") {
		t.Errorf("chunk.Text = %q, want to mention Overloaded", chunk.Text)
	}

	var streamErr *providererrors.StreamProviderError
	if !errors.As(chunk.Err, &streamErr) {
		t.Fatalf("chunk.Err = %v (%T), want *providererrors.StreamProviderError", chunk.Err, chunk.Err)
	}
	if streamErr.Type != "overloaded_error" {
		t.Errorf("Type = %q, want overloaded_error", streamErr.Type)
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 529 {
		t.Errorf("StatusCode = %v, want 529", streamErr.StatusCode)
	}
	if !streamErr.IsRetryable {
		t.Error("IsRetryable = false, want true for overloaded_error")
	}
	if streamErr.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", streamErr.Provider)
	}
}

// TestStream_AuthenticationErrorEvent ports TS's non-retryable branch:
// authentication_error infers statusCode 401 and isRetryable false.
func TestStream_AuthenticationErrorEvent(t *testing.T) {
	sseData := "" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"Invalid API key\"}}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	var streamErr *providererrors.StreamProviderError
	if !errors.As(chunk.Err, &streamErr) {
		t.Fatalf("chunk.Err = %v (%T), want *providererrors.StreamProviderError", chunk.Err, chunk.Err)
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 401 {
		t.Errorf("StatusCode = %v, want 401", streamErr.StatusCode)
	}
	if streamErr.IsRetryable {
		t.Error("IsRetryable = true, want false for authentication_error")
	}
}

// TestStream_ErrorEventExplicitOverridesInference ports TS's `error.statusCode
// ?? inferredMetadata.statusCode` / `error.isRetryable ?? inferredMetadata.isRetryable`:
// an explicit statusCode/isRetryable on the wire event takes precedence over
// the type-inferred defaults.
func TestStream_ErrorEventExplicitOverridesInference(t *testing.T) {
	sseData := "" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"custom\",\"statusCode\":599,\"isRetryable\":false}}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	var streamErr *providererrors.StreamProviderError
	if !errors.As(chunk.Err, &streamErr) {
		t.Fatalf("chunk.Err = %v (%T), want *providererrors.StreamProviderError", chunk.Err, chunk.Err)
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 599 {
		t.Errorf("StatusCode = %v, want explicit 599 (not inferred 529)", streamErr.StatusCode)
	}
	if streamErr.IsRetryable {
		t.Error("IsRetryable = true, want explicit false (not inferred true)")
	}
}

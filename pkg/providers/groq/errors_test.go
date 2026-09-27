package groq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported (behaviorally) from ai/packages/groq/src/groq-error.ts's
// {"error":{"message","type"}} envelope handling, previously entirely
// unimplemented in Go (handleError always returned StatusCode 0 and the raw
// "HTTP <code>: <body>" text).

func TestGroqErrorEnvelopeIsParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("bad-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if providerErr.Provider != "groq" || providerErr.StatusCode != http.StatusBadRequest ||
		providerErr.ErrorCode != "invalid_request_error" || providerErr.Message != "model not found" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}

func TestGroqErrorFallsBackForNonEnvelopeBodyPreservesStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("bad-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	// A StatusCode of 0 is treated as "unknown"/retryable; a genuine 401 must
	// not be misclassified this way.
	if providerErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", providerErr.StatusCode, http.StatusUnauthorized)
	}
}

// P1-1c part 2: a mid-stream `error` frame's type must be mapped through
// groqStreamErrorMetadata (ported from TS getGroqStreamErrorMetadata) into a
// structured StreamProviderError, not just a bare message.

func TestGroqStreamErrorRateLimitIsRetryable(t *testing.T) {
	se := newGroqStreamProviderErrorChunk([]byte(`{"message":"Rate limited","type":"rate_limit_error"}`))
	if se.Message != "Rate limited" {
		t.Errorf("Message = %q, want Rate limited", se.Message)
	}
	if se.Type != "rate_limit_error" {
		t.Errorf("Type = %q, want rate_limit_error", se.Type)
	}
	if se.StatusCode == nil || *se.StatusCode != 429 {
		t.Errorf("StatusCode = %v, want 429", se.StatusCode)
	}
	if !se.IsRetryable {
		t.Error("IsRetryable = false, want true for rate_limit_error")
	}
}

func TestGroqStreamErrorInvalidRequestNotRetryable(t *testing.T) {
	se := newGroqStreamProviderErrorChunk([]byte(`{"message":"bad input","type":"invalid_request_error"}`))
	if se.StatusCode == nil || *se.StatusCode != 400 {
		t.Errorf("StatusCode = %v, want 400", se.StatusCode)
	}
	if se.IsRetryable {
		t.Error("IsRetryable = true, want false for invalid_request_error")
	}
}

// TestGroqStream_ErrorChunkAttachesStructuredPayload exercises the full
// stream path (SSE `error` field on a chunk) end to end.
func TestGroqStream_ErrorChunkAttachesStructuredPayload(t *testing.T) {
	sseData := `data: {"error":{"message":"Service unavailable","type":"service_unavailable"}}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("chunk.Type = %v, want ChunkTypeError", chunk.Type)
	}
	var streamErr *providererrors.StreamProviderError
	if !errors.As(chunk.Err, &streamErr) {
		t.Fatalf("chunk.Err = %v (%T), want *providererrors.StreamProviderError", chunk.Err, chunk.Err)
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 503 {
		t.Errorf("StatusCode = %v, want 503", streamErr.StatusCode)
	}
	if !streamErr.IsRetryable {
		t.Error("IsRetryable = false, want true for service_unavailable")
	}
}

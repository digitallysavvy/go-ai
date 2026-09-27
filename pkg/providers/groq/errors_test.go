package groq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

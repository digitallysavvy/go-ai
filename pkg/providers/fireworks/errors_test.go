package fireworks

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

// Ported (behaviorally) from ai/packages/fireworks/src/fireworks-provider.ts's
// object error envelope handling (550c127).

func TestFireworksErrorEnvelopeIsParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"object":"error","type":"invalid_request_error","code":"bad_request","message":"model not found"}}`))
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
	if providerErr.Provider != "fireworks" || providerErr.StatusCode != http.StatusBadRequest || providerErr.ErrorCode != "bad_request" || providerErr.Message != "model not found" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}

func TestFireworksErrorFallsBackForNonEnvelopeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
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
	// TS createJsonErrorResponseHandler always sets statusCode: response.status
	// even in its catch-all branch; the fallback must not hardcode 0.
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if providerErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want %d", providerErr.StatusCode, http.StatusInternalServerError)
	}
}

// TestFireworksErrorEnvelopeStringErrorIsParsed guards TS fireworksErrorSchema's
// `error: z.union([z.string(), z.object({...})])`: a bare string error value
// must still be parsed into the message, not silently dropped because it
// fails to unmarshal into the object shape.
func TestFireworksErrorEnvelopeStringErrorIsParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("bad-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if providerErr.Message != "model not found" {
		t.Fatalf("Message = %q, want %q", providerErr.Message, "model not found")
	}
}

// TestFireworksErrorEnvelopeNumericCodeIsParsed guards TS fireworksErrorSchema's
// `code: z.union([z.string(), z.number()])`: a numeric code must not break
// decoding the whole envelope (a string-only Code field would fail the
// unmarshal outright, dropping the parsed message too).
func TestFireworksErrorEnvelopeNumericCodeIsParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"object":"error","type":"invalid_request_error","code":400,"message":"model not found"}}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("bad-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if providerErr.Message != "model not found" || providerErr.ErrorCode != "400" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}

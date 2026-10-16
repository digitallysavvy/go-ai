package stability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS postJsonToApi lets a transport failure propagate unchanged.
func TestImageTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).ImageModel("")
	server.Close()
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %#v", err)
	}
}

// An HTTP error response is a ProviderError (status, retryability, headers and
// body kept) carrying the API's message, not a bare transport status error.
func TestR33_HTTPErrorIsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "r1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"id":"i","name":"rate_limited","message":"slow down"}`))
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).ImageModel("")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 429 || perr.Message != "slow down" || !perr.IsRetryable() || perr.ResponseHeaders["X-Request-Id"] != "r1" {
		t.Fatalf("err = %#v", err)
	}
}

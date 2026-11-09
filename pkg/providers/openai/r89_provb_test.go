package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-responses-language-model.ts 872-895: the APICallError of a
// response with an error object (not retryable) or without output carries
// responseHeaders and responseBody.
func TestR89provbResponsesEmbeddedErrorKeepsResponse(t *testing.T) {
	for name, body := range map[string]string{
		"error":     `{"id":"r","error":{"message":"boom","code":"server_error","type":"server_error"}}`,
		"no output": `{"id":"r"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req_1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		lm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-5")
		if err != nil {
			t.Fatal(err)
		}
		_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.ResponseHeaders["x-request-id"] != "req_1" || pe.ResponseBody != body || pe.IsRetryable() {
			t.Errorf("%s: err = %#v, want response headers and body, not retryable", name, err)
		}
	}
}

// TS openaiFailedResponseHandler for the decision model: headers, body and
// data are kept; an empty message is a message; a non-matching body gives
// the status text.
func TestR89provbDecisionModelAPIError(t *testing.T) {
	m := NewDecisionModel(New(Config{APIKey: "k"}), "gpt-5")
	for body, want := range map[string]string{
		`{"error":{"message":"slow","code":"rate"}}`: "slow",
		`{"error":{"message":""}}`:                   "",
		`<html/>`:                                    "Too Many Requests",
	} {
		cause := &internalhttp.HTTPStatusError{StatusCode: 429, Headers: http.Header{"Retry-After": {"1"}}, Body: []byte(body)}
		var pe *providererrors.ProviderError
		if err := m.handleError(cause); !errors.As(err, &pe) || pe.StatusCode != 429 || pe.Message != want || pe.ResponseHeaders["Retry-After"] != "1" || pe.ResponseBody != body {
			t.Errorf("%s: err = %#v", body, err)
		}
	}
}

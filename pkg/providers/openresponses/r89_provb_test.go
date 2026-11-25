package openresponses

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

// TS open-responses-language-model.ts doGenerate: the APICallError of a
// response with an error object, or without output, carries responseHeaders
// and responseBody.
func TestR89provbEmbeddedErrorKeepsResponse(t *testing.T) {
	for name, body := range map[string]string{
		"error":     `{"id":"r","error":{"code":"server_error","message":"boom"}}`,
		"no output": `{"id":"r","status":"failed"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req_1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		lm, err := New(Config{BaseURL: srv.URL}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.ResponseHeaders["x-request-id"] != "req_1" || pe.ResponseBody != body {
			t.Errorf("%s: err = %#v, want response headers and body", name, err)
		}
	}
}

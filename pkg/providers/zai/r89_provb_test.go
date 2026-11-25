package zai

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

// TS createJsonErrorResponseHandler (zaiErrorSchema): the APICallError keeps
// the response headers, body and data, for a matching body and for the
// status-text fallback alike, so retry-after drives the retry delay.
func TestR89provbAPIErrorKeepsResponse(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"slow down","code":"1302"}}`: "slow down",
		`<html>busy</html>`: "Too Many Requests",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After-Ms", "10")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(body))
		}))
		lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("glm-4.6")
		_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 429 || pe.Message != want || pe.ResponseHeaders["Retry-After-Ms"] != "10" || pe.ResponseBody != body {
			t.Errorf("%s: err = %#v", body, err)
		}
	}
}

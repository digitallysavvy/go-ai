package moonshot

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

// provg CR r89 (TS createJsonErrorResponseHandler, moonshotAIErrorSchema):
// the APICallError keeps the response headers, so retry-after drives the
// retry delay, for a schema body and for a non-schema body alike.
func TestR89provbAPIErrorKeepsHeaders(t *testing.T) {
	for _, body := range []string{`{"error":{"message":"slow","type":"rate_limit_error"}}`, `<html/>`, ``} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(body))
		}))
		lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
		_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 429 || pe.ResponseHeaders["retry-after"] != "7" || pe.ResponseBody != body {
			t.Errorf("%q: err = %#v", body, err)
		}
	}
}

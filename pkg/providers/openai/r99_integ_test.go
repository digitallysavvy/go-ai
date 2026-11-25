package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cr-r99-tools-to-provc / cr-r99-provb-to-integrator: TS
// createBinaryResponseHandler fails a null body (fetch gives a 204 / 205 one)
// with the APICallError "Response body is empty" (status, headers, no body).
// openai-compatible-image-model.ts URL-only image download.
func TestR99integCompatImageDownloadNullBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/images/generations" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"b64_json":null,"url":"` + srv.URL + `/img.png"}]}`))
				return
			}
			w.WriteHeader(status)
		}))
		p := New(Config{APIKey: "k", Name: "acme", BaseURL: srv.URL + "/v1", OpenAICompatible: true})
		_, err := NewImageModel(p, "m").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != status {
			t.Errorf("%d: err = %v, want Response body is empty", status, err)
		}
	}
}

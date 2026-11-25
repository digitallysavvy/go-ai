package xai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cr-r99-tools-to-provc / cr-r99-provb-to-integrator: TS
// createBinaryResponseHandler fails a null body (fetch gives a 204 / 205 one)
// with the APICallError "Response body is empty" (status, headers, no body).
// The image model and the batch image results share downloadImage.
func TestR99integImageDownloadNullBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-image")
		_, err := m.downloadImage(context.Background(), srv.URL+"/i.png", "xai.image")
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != status || pe.Provider != "xai.image" {
			t.Errorf("%d: err = %v, want Response body is empty", status, err)
		}
	}
}

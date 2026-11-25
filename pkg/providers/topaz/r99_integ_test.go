package topaz

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cr-r99-tools-to-provc / cr-r99-provb-to-integrator: TS
// createBinaryResponseHandler fails a null body (fetch gives a 204 / 205 one)
// with the APICallError "Response body is empty" (status, headers, no body).
func TestR99integImageDownloadNullBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := r93toolsImageServer(t, `{"status":"Completed"}`, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }, nil)
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ImageModelWonder35)
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Files:           []provider.ImageFile{inputImageFile()},
			ProviderOptions: map[string]interface{}{"topaz": map[string]interface{}{"pollIntervalMillis": 1}},
		})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != status {
			t.Errorf("%d: err = %v, want Response body is empty", status, err)
		}
	}
}

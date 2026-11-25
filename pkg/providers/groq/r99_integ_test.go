package groq

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
// groqTextTranscriptionResponseHandler decodes createBinaryResponseHandler's
// value.
func TestR99integTextTranscriptionNullBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "whisper-large-v3")
		_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte{1, 2}, MimeType: "audio/wav",
			ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"responseFormat": "text"}},
		})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != status {
			t.Errorf("%d: err = %v, want Response body is empty", status, err)
		}
	}
}

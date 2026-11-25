package google

import (
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS googleFailedResponseHandler is shared by embeddings, speech, files and
// interactions: a body outside the Google error schema gives the status text.
func TestGoogleSiblingErrorHandlersUseStatusText(t *testing.T) {
	status := &internalhttp.HTTPStatusError{StatusCode: 503, Body: []byte(`<html>oops</html>`)}
	ok := &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`)}
	handlers := map[string]func(error) error{
		"embedding":    (&EmbeddingModel{}).handleError,
		"speech":       (&SpeechModel{}).handleError,
		"interactions": (&InteractionsLanguageModel{}).handleError,
		"files": func(e error) error {
			se := e.(*internalhttp.HTTPStatusError)
			return (&Provider{}).handleFilesError(se.StatusCode, http.Header{}, se.Body)
		},
	}
	for name, h := range handlers {
		var pe *providererrors.ProviderError
		if err := h(status); !errors.As(err, &pe) || pe.Message != "Service Unavailable" || pe.ResponseBody != `<html>oops</html>` {
			t.Errorf("%s: got %v", name, err)
		}
		if err := h(ok); !errors.As(err, &pe) || pe.Message != "bad" {
			t.Errorf("%s: schema body got %v", name, err)
		}
	}
}

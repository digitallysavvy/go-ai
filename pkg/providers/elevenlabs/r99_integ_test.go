package elevenlabs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cr-r99-tools-to-provc / cr-r99-provb-to-integrator: TS
// createBinaryResponseHandler fails a null body (fetch gives a 204 / 205 one)
// with the APICallError "Response body is empty" (status, headers, no body).
func TestR99integSpeechNullBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		p := newElevenLabsProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": {"r1"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		_, err := NewSpeechModel(p, "eleven_multilingual_v2").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != status || pe.ResponseHeaders["x-request-id"] != "r1" {
			t.Errorf("%d: err = %v, want Response body is empty", status, err)
		}
	}
}

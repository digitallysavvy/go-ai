package azure

import (
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// provg CR r89 (TS createJsonErrorResponseHandler and the Azure Speech / MAI
// failed-response handlers): Azure API errors keep the response headers and
// body, so retry-after drives the retry delay.
func TestR89provbAzureAPIErrorsKeepResponse(t *testing.T) {
	h := http.Header{"Retry-After": {"7"}}
	body := `{"error":{"message":"slow"}}`
	for name, err := range map[string]error{
		"speech transcription": azureSpeechAPIError("azure.transcription", 429, h, []byte(body), false),
		"speech synthesis":     azureSpeechAPIError("azure.speech", 429, h, []byte(body), true),
		"mai image":            azureMaiImageAPIError(429, providerutils.ExtractHeaders(h), []byte(body)),
	} {
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 429 || pe.Message != "slow" || pe.ResponseHeaders["Retry-After"] != "7" || pe.ResponseBody != body {
			t.Errorf("%s: err = %#v", name, err)
		}
	}
}

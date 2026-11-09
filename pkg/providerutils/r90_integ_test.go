package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cr-r90-provb-to-integrator 1: TS createStatusCodeErrorResponseHandler
// builds APICallError({message: statusText, statusCode, responseHeaders,
// responseBody}) with no data, even for a JSON body.
func TestR90integStatusCodeDownloadErrorHasNoData(t *testing.T) {
	err := StatusCodeDownloadError("p", &providererrors.DownloadError{StatusCode: 404, Body: []byte(`{"error":"x"}`)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T %v, want *ProviderError", err, err)
	}
	if pe.Message != "Not Found" || pe.Data != nil || pe.StatusCode != 404 || string(pe.ResponseBody) != `{"error":"x"}` {
		t.Fatalf("ProviderError = message %q data %#v status %d body %q", pe.Message, pe.Data, pe.StatusCode, pe.ResponseBody)
	}
}

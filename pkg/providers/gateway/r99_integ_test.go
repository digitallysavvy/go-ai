package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS gateway-video-model.ts: fetch gives a 204 / 205 a null body, and the
// SSE handler throws the APICallError "SSE response body is empty" with the
// status, which asGatewayError reports as the response error of that status
// (Go returned DoStream's "Failed to process successful response").
func TestR99integVideoNullBodyIsSSEBodyEmpty(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewVideoModel(p, "google/veo-3.1").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
		srv.Close()
		var re *gatewayerrors.GatewayResponseError
		var pe *providererrors.ProviderError
		if !errors.As(err, &re) || re.GetStatusCode() != status || !errors.As(err, &pe) || pe.Message != "SSE response body is empty" || pe.StatusCode != status {
			t.Errorf("%d: err = %T %v, want the gateway response error of the SSE response body is empty APICallError", status, err, err)
		}
	}
}

// TS createJsonResponseHandler with experimental_binaryFields reads a null
// body (204 / 205) as no bytes (readResponseWithSizeLimit), so the call
// fails with "Invalid JSON response", as without streaming decoding.
func TestR99integStreamingImageNullBodyIsInvalidJSON(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		model := NewImageModel(p, "openai/gpt-image-1")
		for _, on := range []bool{false, true} {
			_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p",
				ProviderOptions: map[string]interface{}{"gateway": map[string]interface{}{"experimental_streamingImageDecoding": on}}})
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != providererrors.InvalidJSONResponseMessage || pe.StatusCode != status {
				t.Errorf("%d streaming=%v: err = %T %v (%+v), want the Invalid JSON response APICallError", status, on, err, err, pe)
			}
		}
		srv.Close()
	}
}

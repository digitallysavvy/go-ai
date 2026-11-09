package xai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r89provbServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "10")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r89provbAPIError(t *testing.T, err error) *providererrors.ProviderError {
	t.Helper()
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a ProviderError", err)
	}
	return pe
}

// TS xai-error.ts xaiFailedResponseHandler (createJsonErrorResponseHandler):
// every xAI model's API error is an APICallError with the response headers
// (retry-after-ms drives the retry delay), the body and the parsed data; a
// body matching no xaiErrorDataSchema member gives the status text; the
// responses shape gives "code: error".
func TestR89provbAPIErrorsKeepResponse(t *testing.T) {
	ctx := context.Background()
	check := func(name string, err error, wantStatus int, wantMessage string) {
		t.Helper()
		pe := r89provbAPIError(t, err)
		if pe.StatusCode != wantStatus || pe.Message != wantMessage || pe.ResponseHeaders["retry-after-ms"] != "10" || pe.ResponseBody == "" {
			t.Errorf("%s: error = status %d message %q headers %v body %q; want status %d message %q with headers and body",
				name, pe.StatusCode, pe.Message, pe.ResponseHeaders, pe.ResponseBody, wantStatus, wantMessage)
		}
	}

	srv := r89provbServer(t, 429, `{"error":{"message":"slow down","type":"rate_limit"}}`)
	_, err := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-image").DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p"})
	check("image", err, 429, "slow down")
	if pe := r89provbAPIError(t, err); pe.Data == nil {
		t.Errorf("image: Data = nil, want the parsed body")
	}

	srv = r89provbServer(t, 502, `<html>bad gateway</html>`)
	_, err = NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-image").DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p"})
	check("image non-schema body", err, 502, "Bad Gateway")

	srv = r89provbServer(t, 400, `{"code":"invalid_argument","error":"bad model"}`)
	lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("grok-4")
	_, err = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	check("responses", err, 400, "invalid_argument: bad model")

	srv = r89provbServer(t, 429, `{"error":"too many"}`)
	op, _ := json.Marshal(xaiVideoOperation{RequestID: "r1"})
	_, err = NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video").DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: op})
	check("video status", err, 429, "too many")

	srv = r89provbServer(t, 429, `{"error":"too many"}`)
	_, err = NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video").DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	check("video start", err, 429, "too many")
}

// provc CR (TS generate-video.ts useStartStatus: XaiVideoModel has no
// doGenerate): the model reports its v0.5.2 poll options to the core
// start/status flow.
func TestR89provbVideoStartStatusPollDefaults(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "grok-imagine-video")
	if i, to := m.VideoStartStatusPollDefaults(nil); i != 0 || to != 0 {
		t.Errorf("empty options = %d, %d; want 0, 0", i, to)
	}
	i, to := m.VideoStartStatusPollDefaults(map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 250, "pollTimeoutMs": 90000}})
	if i != 250 || to != 90000 {
		t.Errorf("poll options = %d, %d; want 250, 90000", i, to)
	}
}

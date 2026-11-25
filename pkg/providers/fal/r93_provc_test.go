package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r93provcHangUp(w http.ResponseWriter) {
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func r93provcIsConnectError(t *testing.T, name string, err error) {
	t.Helper()
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Errorf("%s: %T %v, want the retryable Cannot connect to API error", name, err, err)
	}
}

// TestR93provcGetFromAPINetworkFailureIsConnectError: TS downloads images
// and audio and polls video status with getFromApi, whose handleFetchError
// turns a connection that failed before any response into the retryable
// "Cannot connect to API" APICallError (doStatus reports it as an error
// status). Go returned the DownloadError fileutil wraps it in.
func TestR93provcGetFromAPINetworkFailureIsConnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fal-ai/flux/dev":
			_, _ = w.Write([]byte(`{"images":[{"url":"http://` + r.Host + `/img.png"}]}`))
		case "/speech-model":
			_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/a.mp3"}}`))
		default:
			r93provcHangUp(w)
		}
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	p.speechHost = srv.URL

	_, err := NewImageModel(p, "fal-ai/flux/dev").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	r93provcIsConnectError(t, "image download", err)

	_, err = NewSpeechModel(p, "speech-model").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"})
	r93provcIsConnectError(t, "speech download", err)

	op, _ := json.Marshal(falVideoOperation{ResponseURL: srv.URL + "/requests/1", SubmitURL: srv.URL + "/fal-ai/m"})
	res, err := NewVideoModel(p, "fal-ai/m").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || res.Status != provider.VideoOperationStatusError || !strings.HasPrefix(res.Error, "Cannot connect to API: ") || res.Response.Headers != nil {
		t.Errorf("video status = %+v, %v", res, err)
	}
}

// TestR93provcVideoNullOptionsAreAbsent: TS parseProviderOptions returns
// undefined for a null providerOptions.fal; Go failed with "expected object".
func TestR93provcVideoNullOptionsAreAbsent(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "fal-ai/m")
	body, err := m.buildStartRequestBody(&provider.VideoModelV3CallOptions{Prompt: "x", ProviderOptions: map[string]interface{}{"fal": nil}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(body); string(got) != `{"prompt":"x"}` {
		t.Errorf("body = %s", got)
	}
}

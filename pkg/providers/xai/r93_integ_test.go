package xai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integImageDownloadNetworkFailureIsConnectError
// (cr-r93-provc-to-provb): the image URL download is TS getFromApi
// (xai-image-model.ts), whose handleFetchError turns a connection that
// failed before any response into the retryable "Cannot connect to API"
// APICallError.
func TestR93integImageDownloadNetworkFailureIsConnectError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"url":"` + srv.URL + `/img.png"}]}`))
			return
		}
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"}), ModelGrokImagineImage)
	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Fatalf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}

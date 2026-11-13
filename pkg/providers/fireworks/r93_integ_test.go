package fireworks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integImageDownloadNetworkFailureIsConnectError
// (cr-r93-provc-to-provb): the result.sample download is TS getFromApi
// (fireworks-image-model.ts), whose handleFetchError turns a connection
// that failed before any response into the retryable "Cannot connect to
// API" APICallError.
func TestR93integImageDownloadNetworkFailureIsConnectError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/get_result"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"r1","status":"Ready","result":{"sample":"` + srv.URL + `/img.png"}}`))
		case r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
		default:
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "accounts/fireworks/models/flux-kontext-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Fatalf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}

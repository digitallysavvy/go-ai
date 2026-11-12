package topaz

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integImageDownloadNetworkFailureIsConnectError
// (cr-r93-provc-to-tools): the download_url fetch is TS getFromApi, whose
// handleFetchError turns a connection that failed before any response into
// the retryable "Cannot connect to API" APICallError (Go returned the
// fileutil DownloadError).
func TestR93integImageDownloadNetworkFailureIsConnectError(t *testing.T) {
	srv := r93toolsImageServer(t, `{"status":"Completed"}`, func(w http.ResponseWriter, _ *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}, nil)
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ImageModelWonder35)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Files:           []provider.ImageFile{inputImageFile()},
		ProviderOptions: map[string]interface{}{"topaz": map[string]interface{}{"pollIntervalMillis": 1}},
	})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Fatalf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}

package heygen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93integStatusNetworkFailureIsConnectError (cr-r93-provc-to-provg):
// the status poll is TS getFromApi, whose handleFetchError turns a
// connection that failed before any response into the retryable "Cannot
// connect to API" APICallError (Go returned the fileutil DownloadError).
func TestR93integStatusNetworkFailureIsConnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "heygen-video-1")
	_, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Fatalf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}

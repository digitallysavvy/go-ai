package gladia

import (
	"context"
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

// TestR93provcGetFromAPINetworkFailureIsConnectError: TS polls (or downloads)
// with getFromApi, whose handleFetchError turns a connection that failed
// before any response into the retryable "Cannot connect to API"
// APICallError. Go returned the DownloadError fileutil wraps it in.
func TestR93provcGetFromAPINetworkFailureIsConnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_, _ = w.Write([]byte(`{"audio_url":"https://x/a"}`))
		case "/pre-recorded":
			_, _ = w.Write([]byte(`{"result_url":"http://` + r.Host + `/pre-recorded/1"}`))
		default:
			r93provcHangUp(w)
		}
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("default")
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Errorf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}

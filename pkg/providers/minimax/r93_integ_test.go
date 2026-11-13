package minimax

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r93integHangUp(w http.ResponseWriter) {
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		_ = conn.Close()
	}
}

func r93integIsConnectError(err error) bool {
	r, ok := err.(interface{ IsRetryable() bool })
	return err != nil && strings.HasPrefix(err.Error(), "Cannot connect to API: ") && ok && r.IsRetryable()
}

// TestR93integNetworkFailureIsConnectError (cr-r93-provc-to-tools): the
// video status poll and the image download are TS getFromApi, whose
// handleFetchError turns a connection that failed before any response into
// the retryable "Cannot connect to API" APICallError.
func TestR93integNetworkFailureIsConnectError(t *testing.T) {
	t.Run("video status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"task_id":"t"}`))
				return
			}
			r93integHangUp(w)
		}))
		defer srv.Close()
		model := newVideoModel(providerForMinimaxServer(srv.URL), ModelH3)
		start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *minimaxDefaultCallOptions(nil)})
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		_, err = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
		if !r93integIsConnectError(err) {
			t.Fatalf("err = %T %v, want the retryable Cannot connect to API error", err, err)
		}
	})
	t.Run("image download", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/images/generations" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"url":"` + srv.URL + `/generated.png"}]}`))
				return
			}
			r93integHangUp(w)
		}))
		defer srv.Close()
		m, _ := New(Config{APIKey: "k", ImageBaseURL: srv.URL + "/v1"}).Image(ModelH3Image)
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: r91toolsN(1)})
		if !r93integIsConnectError(err) {
			t.Fatalf("err = %T %v, want the retryable Cannot connect to API error", err, err)
		}
	})
}

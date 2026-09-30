package fileutil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TS 4a67783 "cancel prompt attachment downloads on abort/timeout": the
// request context (the step's abort/timeout context) must cancel an
// in-flight download, both while waiting for headers and while streaming the
// body.
func TestDownloadCancelledByContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	for _, path := range []string{"/headers", "/body"} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		start := time.Now()
		_, err := Download(ctx, srv.URL+path, insecureDownloadOptions())
		cancel()
		if err == nil {
			t.Fatalf("%s: expected cancellation error", path)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: err = %v, want context deadline exceeded in chain", path, err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("%s: download was not cancelled promptly", path)
		}
	}
}

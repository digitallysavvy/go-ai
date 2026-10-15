package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDoStreamIgnoresDefaultClientTimeout: the shared default client's
// whole-request timeout must not cut off a stream that is still flowing.
func TestDoStreamIgnoresDefaultClientTimeout(t *testing.T) {
	orig := DefaultHTTPClient
	DefaultHTTPClient = &http.Client{Timeout: 100 * time.Millisecond}
	defer func() { DefaultHTTPClient = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		for i := 0; i < 4; i++ {
			_, _ = w.Write([]byte("data: x\n\n"))
			fl.Flush()
			time.Sleep(80 * time.Millisecond)
		}
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL})
	resp, err := c.DoStream(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream cut off: %v", err)
	}
	if len(b) != 4*len("data: x\n\n") {
		t.Fatalf("got %d bytes", len(b))
	}
}

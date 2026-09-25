package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ports TS response-handler.test.ts size-limit behavior (6a436e3): JSON/text
// response bodies are read through readResponseWithSizeLimit.
func TestClientDoBoundsResponseBody(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/chunked":
			w.(stdhttp.Flusher).Flush() // no Content-Length
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		case "/declared":
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(stdhttp.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
		case "/ok":
			_, _ = w.Write([]byte(strings.Repeat("x", 16)))
		case "/err":
			w.WriteHeader(stdhttp.StatusBadRequest)
			_, _ = w.Write([]byte(strings.Repeat("e", 64)))
		}
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, MaxResponseBytes: 32})

	for _, path := range []string{"/chunked", "/declared"} {
		_, err := c.Do(context.Background(), Request{Method: "GET", Path: path})
		var de *providererrors.DownloadError
		if !errors.As(err, &de) {
			t.Fatalf("%s: err = %v, want DownloadError", path, err)
		}
		if !strings.Contains(de.Message, "exceeded maximum size of 32 bytes") {
			t.Fatalf("%s: message = %q", path, de.Message)
		}
	}

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/ok"})
	if err != nil || len(resp.Body) != 16 {
		t.Fatalf("ok: resp=%v err=%v", resp, err)
	}

	_, err = c.DoStream(context.Background(), Request{Method: "GET", Path: "/err"})
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || len(statusErr.Body) != 0 {
		t.Fatalf("stream error body should be dropped when over the limit, got %v", err)
	}
}

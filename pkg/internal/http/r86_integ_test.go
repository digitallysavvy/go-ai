package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

// prova CR r86 1 (to carry, applied by the integrator): DoStream's error body
// is TS response.text(), like DoJSON's: the BOM is dropped (and invalid UTF-8
// replaced) before a stream error parser reads it.
func TestR86integDoStreamErrorBodyDecoded(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusBadRequest)
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"error\":{\"message\":\"boom\"}}"))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL})
	_, err := c.DoStream(context.Background(), Request{Method: stdhttp.MethodPost, Path: "/s"})
	var se *HTTPStatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v", err)
	}
	if got := string(se.Body); got != `{"error":{"message":"boom"}}` {
		t.Fatalf("body = %q", got)
	}
}

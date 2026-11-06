package mantle

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type r86provaCloseRecorder struct {
	io.Reader
	closed bool
}

func (r *r86provaCloseRecorder) Close() error { r.closed = true; return nil }

// A failed key func closes the request body (http.RoundTripper contract).
func TestR86provaMantleTransportClosesBodyOnError(t *testing.T) {
	keyErr := errors.New("no key")
	tr := &sigV4Transport{base: http.DefaultTransport, apiKeyFunc: func(context.Context) (string, error) { return "", keyErr }}
	body := &r86provaCloseRecorder{Reader: strings.NewReader("{}")}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", body)
	if _, err := tr.RoundTrip(req); !errors.Is(err, keyErr) {
		t.Fatalf("err = %v", err)
	}
	if !body.closed {
		t.Fatal("request body not closed")
	}
}

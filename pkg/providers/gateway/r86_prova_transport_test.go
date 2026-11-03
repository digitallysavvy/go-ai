package gateway

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

// A failed credential lookup closes the request body (http.RoundTripper
// contract).
func TestR86provaGatewayTransportClosesBodyOnError(t *testing.T) {
	authErr := errors.New("no credentials")
	tr := &gatewayAuthTransport{base: http.DefaultTransport, resolver: func(context.Context) (string, string, error) { return "", "", authErr }}
	body := &r86provaCloseRecorder{Reader: strings.NewReader("{}")}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", body)
	if _, err := tr.RoundTrip(req); !errors.Is(err, authErr) {
		t.Fatalf("err = %v", err)
	}
	if !body.closed {
		t.Fatal("request body not closed")
	}
}

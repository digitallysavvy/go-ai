package azure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

var errR86integ = errors.New("r86 integ failure")

type r86integCloseRecorder struct {
	io.Reader
	closed bool
}

func (r *r86integCloseRecorder) Close() error { r.closed = true; return nil }

// prova CR r86 (RoundTripper class): the transport closes the request body
// when it fails before the base transport runs (http.RoundTripper contract).
func TestR86integTransportClosesBodyOnError(t *testing.T) {
	tr := azureTokenTransport{base: http.DefaultTransport, tokenProvider: func(context.Context) (string, error) { return "", errR86integ }}
	body := &r86integCloseRecorder{Reader: strings.NewReader("{}")}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", body)
	if _, err := tr.RoundTrip(req); err == nil || !strings.Contains(err.Error(), errR86integ.Error()) {
		t.Fatalf("err = %v", err)
	}
	if !body.closed {
		t.Fatal("request body not closed")
	}
}

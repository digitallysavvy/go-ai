package anthropicaws

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

type r86integFailingReader struct{}

func (r86integFailingReader) Read([]byte) (int, error) { return 0, errR86integ }

// prova CR r86 (RoundTripper class): the transport closes the request body
// when it fails before the base transport runs (http.RoundTripper contract).
func TestR86integTransportClosesBodyOnError(t *testing.T) {
	tr := &sigV4Transport{base: http.DefaultTransport, credentialProvider: func(context.Context) (Credentials, error) { return Credentials{}, errR86integ }, region: "us-east-1", baseURL: "http://127.0.0.1:1"}
	body := &r86integCloseRecorder{Reader: r86integFailingReader{}}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", body)
	if _, err := tr.RoundTrip(req); err == nil || !strings.Contains(err.Error(), errR86integ.Error()) {
		t.Fatalf("err = %v", err)
	}
	if !body.closed {
		t.Fatal("request body not closed")
	}
}

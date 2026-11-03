package anthropic

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

// A failed bearer token func closes the request body (http.RoundTripper
// contract).
func TestR86provaBedrockAnthropicTransportClosesBodyOnError(t *testing.T) {
	keyErr := errors.New("no key")
	p := &BedrockAnthropicProvider{config: Config{BearerTokenFunc: func(context.Context) (string, error) { return "", keyErr }}}
	tr := &bedrockAnthropicTransport{base: http.DefaultTransport, provider: p}
	body := &r86provaCloseRecorder{Reader: strings.NewReader("{}")}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", body)
	if _, err := tr.RoundTrip(req); !errors.Is(err, keyErr) {
		t.Fatalf("err = %v", err)
	}
	if !body.closed {
		t.Fatal("request body not closed")
	}
}

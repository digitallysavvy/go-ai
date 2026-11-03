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

// TS createAmazonBedrockAnthropicFetch reads response.text() (BOM dropped)
// and writes JSON.stringify({type, error: {type, message}}); the expected
// text was produced by node with the TS logic.
func TestR86provaTransformErrorBodyMatchesTS(t *testing.T) {
	body := append([]byte("\xEF\xBB\xBF"), []byte(`{"message":"a<b & \"c\" `+" "+`"}`)...)
	want := `{"type":"error","error":{"type":"error","message":"a<b & \"c\" ` + " " + `"}}`
	if got := string(transformErrorBody(body)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got, want := string(transformErrorBody([]byte("\xEF\xBB\xBFnot json"))), `{"type":"error","error":{"type":"error","message":"not json"}}`; got != want {
		t.Fatalf("non-JSON: got %s", got)
	}
}

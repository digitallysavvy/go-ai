package cerebras

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type r86toolsFailingBody struct{ closed bool }

func (b *r86toolsFailingBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (b *r86toolsFailingBody) Close() error             { b.closed = true; return nil }

type r86toolsRecordingTransport struct{ req *http.Request }

func (t *r86toolsRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.req = req
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
}

// prova CR r86 1: a RoundTripper closes the request body also when reading
// it fails, and sends the rewritten body on a clone (the caller's request
// is not modified).
func TestR86toolsTransformTransportBody(t *testing.T) {
	extras := cerebrasRequestExtras{"reasoning_effort": "low"}
	body := &r86toolsFailingBody{}
	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid/v1/chat/completions", body)
	req = req.WithContext(context.WithValue(req.Context(), cerebrasOptionsContextKey{}, extras))
	if _, err := (cerebrasTransformTransport{base: &r86toolsRecordingTransport{}}).RoundTrip(req); err == nil || !body.closed {
		t.Fatalf("err = %v, closed = %v; want the read error and a closed body", err, body.closed)
	}

	base := &r86toolsRecordingTransport{}
	orig := io.NopCloser(strings.NewReader(`{"model":"m"}`))
	req, _ = http.NewRequest(http.MethodPost, "http://example.invalid/v1/chat/completions", orig)
	req = req.WithContext(context.WithValue(req.Context(), cerebrasOptionsContextKey{}, extras))
	if _, err := (cerebrasTransformTransport{base: base}).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if base.req == req || req.Body != orig {
		t.Fatal("the caller's request was modified")
	}
	sent, _ := io.ReadAll(base.req.Body)
	if !strings.Contains(string(sent), `"reasoning_effort":"low"`) {
		t.Fatalf("sent body = %s", sent)
	}
}

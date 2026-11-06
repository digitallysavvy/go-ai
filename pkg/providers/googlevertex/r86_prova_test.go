package googlevertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/internal"
)

type r86provaCloseRecorder struct {
	io.Reader
	closed bool
}

func (r *r86provaCloseRecorder) Close() error { r.closed = true; return nil }

// r86provaAssertClosesBody runs rt on a request whose credential step fails
// and checks that the request body was closed (http.RoundTripper contract).
func r86provaAssertClosesBody(t *testing.T, name string, rt http.RoundTripper, want error) {
	t.Helper()
	body := &r86provaCloseRecorder{Reader: strings.NewReader("{}")}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/x", body)
	if _, err := rt.RoundTrip(req); want != nil && !errors.Is(err, want) {
		t.Fatalf("%s: err = %v", name, err)
	}
	if !body.closed {
		t.Fatalf("%s: request body not closed", name)
	}
}

// The credential transports close the request body when they fail before
// the base transport, so a streamed upload body is released.
func TestR86provaVertexTransportsCloseBodyOnError(t *testing.T) {
	keyErr := errors.New("no key")
	failing := func(context.Context) (string, error) { return "", keyErr }
	r86provaAssertClosesBody(t, "express", &vertexExpressTransport{base: http.DefaultTransport, apiKeyFunc: failing}, keyErr)
	r86provaAssertClosesBody(t, "oauth", &internal.AuthTransport{Base: http.DefaultTransport, TokenFunc: failing}, keyErr)
	r86provaAssertClosesBody(t, "maas", &maasAuthTransport{base: http.DefaultTransport, authToken: failing}, keyErr)
	r86provaAssertClosesBody(t, "failing", failingRoundTripper{keyErr}, keyErr)
}

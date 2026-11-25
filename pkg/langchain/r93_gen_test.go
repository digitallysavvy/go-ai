package langchain

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// Round 93 gen tests (TS 2ebc78b654; @langchain/langgraph-sdk 1.14.0).

// langgraph-sdk builds the request URL with new URL(`${apiUrl}${path}`): a URL
// without a scheme is a TypeError "Invalid URL" (node: new
// URL("my-deployment.app/runs/stream")), not a connection error.
func TestR93genRunStreamInvalidURL(t *testing.T) {
	for _, url := range []string{"my-deployment.app", "http://[::1"} {
		transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: url, HTTPClient: &http.Client{Transport: r93genFailTransport{t}}})
		chunks, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{})
		_, gotErrs := drain(t, chunks, errs)
		if len(gotErrs) != 1 || gotErrs[0].Error() != "Invalid URL" {
			t.Errorf("URL %q: errors = %v, want [Invalid URL]", url, gotErrs)
		}
	}
}

// The request goes to the URL's WHATWG serialization (node new URL().href).
func TestR93genRunStreamURLSerialized(t *testing.T) {
	var gotPath string
	server := r91genSSEServer(t, func(r *http.Request) { gotPath = r.URL.EscapedPath() })
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL + "/api/../a b"})
	if errs := r91genSend(t, transport); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// node: new URL("http://h/api/../a b/runs/stream").pathname
	if want := "/a%20b/runs/stream"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

type r93genFailTransport struct{ t *testing.T }

func (f r93genFailTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("unexpected request")
	return nil, http.ErrHandlerTimeout
}

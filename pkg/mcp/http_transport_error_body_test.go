package mcp

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type staticSSEClient struct {
	status      int
	contentType string
	body        io.Reader
}

func (c *staticSSEClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Header:     http.Header{"Content-Type": []string{c.contentType}},
		Body:       io.NopCloser(c.body),
	}, nil
}

// TS send(): a failed HTTP response to a request whose body is a JSON-RPC
// error is delivered as that request's response (null id -> request id).
func TestHTTPTransportDeliversJSONRPCErrorFromFailedResponse(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: &staticSSEClient{status: 400, contentType: "application/json", body: strings.NewReader(`{"jsonrpc":"2.0","id":null,"error":{"code":-32602,"message":"bad params"}}`)},
	})
	transport.connected = true
	msg, _ := CreateRequest(7, "tools/call", nil)
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send returned %v, want the JSON-RPC error delivered as a response", err)
	}
	got, err := transport.Receive(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Error == nil || got.Error.Code != -32602 || got.ID != 7 {
		t.Fatalf("received %+v", got)
	}

	// A plain-text failure body is still a transport error.
	plain := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: &staticSSEClient{status: 500, contentType: "text/plain", body: strings.NewReader("boom")},
	})
	plain.connected = true
	if err := plain.Send(t.Context(), msg); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}
}

// Untrusted response bodies are read with a bound.
func TestHTTPTransportBoundsResponseBodies(t *testing.T) {
	huge := io.LimitReader(zeroReader{}, mcpMaxResponseBodyBytes+10)
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: &staticSSEClient{status: 200, contentType: "application/json", body: huge},
	})
	transport.connected = true
	msg, _ := CreateRequest(1, "ping", nil)
	if err := transport.Send(t.Context(), msg); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size-limit error", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

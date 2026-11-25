package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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

// TS send() (d55f3cfc45): a failed HTTP response to a request whose body is a
// JSON-RPC error rejects the send with an MCPClientError carrying the error,
// the HTTP status, URL and body; it is not reported through OnError and
// nothing is queued as a response.
func TestHTTPTransportRejectsWithJSONRPCErrorFromFailedResponse(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"bad params","data":{"reason":"x"}}}`
	reported := 0
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		OnError:   func(error) { reported++ },
		SSEClient: &staticSSEClient{status: 400, contentType: "application/json", body: strings.NewReader(body)},
	})
	transport.connected = true
	msg, _ := CreateRequest(7, "tools/call", nil)
	err := transport.Send(t.Context(), msg)
	var ce *MCPClientError
	if !errors.As(err, &ce) {
		t.Fatalf("Send returned %v, want an MCPClientError", err)
	}
	if ce.Message != "bad params" || ce.Code != -32602 || ce.StatusCode != 400 || ce.URL != "http://localhost:9999/mcp" || ce.ResponseBody != body {
		t.Fatalf("error = %+v", ce)
	}
	var cause *MCPError
	if !errors.As(ce.Cause, &cause) || cause.Code != -32602 {
		t.Fatalf("cause = %#v", ce.Cause)
	}
	if reported != 0 {
		t.Fatalf("OnError called %d times, want 0", reported)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if got, _ := transport.Receive(ctx); got != nil {
		t.Fatalf("queued %+v", got)
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

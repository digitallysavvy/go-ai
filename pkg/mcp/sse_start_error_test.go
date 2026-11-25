package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type sseStatusClient struct {
	status int
	body   string
}

func (c *sseStatusClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(c.body)),
	}, nil
}

// TS #19430: a non-2xx SSE start response keeps its body verbatim in the
// message (no trimming) and on statusCode / url / responseBody.
func TestSSETransportStartErrorKeepsResponseBody(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, "nope\n", "MCP SSE Transport Error: 401 Unauthorized: nope\n"},
		{http.StatusInternalServerError, "", "MCP SSE Transport Error: 500 Internal Server Error"},
		{http.StatusMethodNotAllowed, "", "MCP SSE Transport Error: 405 Method Not Allowed. This server does not support SSE transport. Try using `http` transport instead"},
	}
	for _, tc := range cases {
		tr := NewSSETransport(SSETransportConfig{
			URL:       "http://localhost:3000/sse",
			SSEClient: &sseStatusClient{status: tc.status, body: tc.body},
		})
		err := tr.Connect(context.Background())
		var ce *MCPClientError
		if !errors.As(err, &ce) {
			t.Fatalf("%d: err = %T %v", tc.status, err, err)
		}
		if ce.Message != tc.want {
			t.Errorf("%d: message = %q, want %q", tc.status, ce.Message, tc.want)
		}
		if ce.StatusCode != tc.status || ce.URL != "http://localhost:3000/sse" || ce.ResponseBody != tc.body {
			t.Errorf("%d: status/url/body = %d %q %q", tc.status, ce.StatusCode, ce.URL, ce.ResponseBody)
		}
	}
}

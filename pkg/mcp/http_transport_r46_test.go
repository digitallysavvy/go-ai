package mcp

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rawStatusServer answers every request with statusLine and an empty body,
// so the reason phrase can differ from Go's canonical http.StatusText.
func rawStatusServer(t *testing.T, statusLine string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close() //nolint:errcheck
				if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
					return
				}
				_, _ = conn.Write([]byte(statusLine + "\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			}(conn)
		}
	}()
	return "http://" + ln.Addr().String() + "/mcp"
}

// TS reports `${response.status} ${response.statusText}`, and fetch's
// statusText is the server's reason phrase, not a canonical one.
func TestHTTPTransportGetSSEFailureUsesServerReasonPhrase(t *testing.T) {
	url := rawStatusServer(t, "HTTP/1.1 503 Busy Right Now")
	errs := make(chan error, 4)
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:                    url,
		InitialProtocolVersion: LatestLegacyProtocolVersion,
		OnError:                func(err error) { errs <- err },
	})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer transport.Close() //nolint:errcheck
	select {
	case err := <-errs:
		if got, want := err.Error(), "MCP HTTP Transport Error: GET SSE failed: 503 Busy Right Now"; got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no GET SSE error reported")
	}
}

func TestSSETransportConnectFailureUsesServerReasonPhrase(t *testing.T) {
	url := rawStatusServer(t, "HTTP/1.1 503 Busy Right Now")
	transport := NewSSETransport(SSETransportConfig{URL: url})
	err := transport.Connect(context.Background())
	if err == nil {
		_ = transport.Close()
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(err.Error(), "MCP SSE Transport Error: 503 Busy Right Now") {
		t.Fatalf("error = %q", err.Error())
	}
}

// A status line without a reason phrase gives fetch statusText "", so TS
// reports "503 " with no canonical text.
func TestSSETransportConnectFailureWithoutReasonPhrase(t *testing.T) {
	url := rawStatusServer(t, "HTTP/1.1 503")
	transport := NewSSETransport(SSETransportConfig{URL: url})
	err := transport.Connect(context.Background())
	if err == nil {
		_ = transport.Close()
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(err.Error(), "MCP SSE Transport Error: 503 ") || strings.Contains(err.Error(), "Service Unavailable") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestResponseStatusText(t *testing.T) {
	cases := []struct {
		status string
		code   int
		want   string
	}{
		{"503 Busy Right Now", 503, "Busy Right Now"},
		{"503", 503, ""},
		{"Service Unavailable", 503, "Service Unavailable"}, // custom SSEClient
		{"", 404, "Not Found"},
	}
	for _, c := range cases {
		if got := responseStatusText(&http.Response{Status: c.status, StatusCode: c.code}); got != c.want {
			t.Errorf("responseStatusText(%q, %d) = %q, want %q", c.status, c.code, got, c.want)
		}
	}
}

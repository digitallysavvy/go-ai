package websocket

import (
	"context"
	"crypto/sha1" //nolint:gosec // RFC 6455 handshake
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"
)

// rawCloseServer completes the handshake, then writes frames verbatim.
func rawCloseServer(t *testing.T, frames ...[]byte) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
			base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
		for _, f := range frames {
			_, _ = rw.Write(f)
		}
		_ = rw.Flush()
	}))
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http")
}

func closeFrame(code uint16, reason string) []byte {
	p := append([]byte{byte(code >> 8), byte(code)}, reason...)
	return append([]byte{0x88, byte(len(p))}, p...)
}

func dialInfo(t *testing.T, url string) *CloseInfo {
	t.Helper()
	info := &CloseInfo{}
	conn, err := Dial(context.Background(), url, DialOptions{WrapConn: info.Wrap})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck
	for {
		var s string
		if err := websocket.Message.Receive(conn, &s); err != nil {
			return info
		}
	}
}

func TestCloseInfo_CodeAndReason(t *testing.T) {
	text := []byte{0x81, 2, 'h', 'i'}
	info := dialInfo(t, rawCloseServer(t, text, closeFrame(4001, "quota exceeded")))
	if info.Code() != 4001 || info.Reason() != "quota exceeded" {
		t.Fatalf("got %d %q", info.Code(), info.Reason())
	}
	if got := info.Suffix(); got != " (code 4001: quota exceeded)" {
		t.Fatalf("suffix = %q", got)
	}
}

func TestCloseInfo_NoReasonNoStatusNoFrame(t *testing.T) {
	if got := dialInfo(t, rawCloseServer(t, closeFrame(1000, ""))).Suffix(); got != " (code 1000)" {
		t.Fatalf("suffix = %q", got)
	}
	if got := dialInfo(t, rawCloseServer(t, []byte{0x88, 0})).Suffix(); got != " (code 1005)" {
		t.Fatalf("no-status suffix = %q", got)
	}
	if got := dialInfo(t, rawCloseServer(t)).Suffix(); got != " (code 1006)" {
		t.Fatalf("no-frame suffix = %q", got)
	}
}

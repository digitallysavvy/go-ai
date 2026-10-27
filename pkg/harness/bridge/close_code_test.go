package bridge_test

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// rawCloseServer accepts one WebSocket upgrade, then writes closeFrame (nil
// drops the TCP connection without a close frame).
func rawCloseServer(t *testing.T, closeFrame []byte) harness.PortEndpoint {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		req, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			return
		}
		h := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		_, _ = c.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
			base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n"))
		if closeFrame != nil {
			_, _ = c.Write(closeFrame)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return harness.PortEndpoint{URL: "ws://" + ln.Addr().String() + "/"}
}

// TS takes the code from the `ws` close event: 1005 for a close frame
// without a status code, 1006 when the connection ends without one.
func TestDial_CloseBeforeHelloUsesWSCloseCodes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame []byte
		want  string
	}{
		{"no status", []byte{0x88, 0x00}, "The harness bridge closed the WebSocket (code 1005) before sending bridge-hello."},
		{"no frame", nil, "The harness bridge closed the WebSocket (code 1006) before sending bridge-hello."},
		{"status", []byte{0x88, 0x02, 0x03, 0xf3}, "The harness bridge closed the WebSocket (code 1011) before sending bridge-hello."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := rawCloseServer(t, tc.frame)
			_, err := bridge.Dial(context.Background(), endpoint, bridge.DialOptions{WaitForHello: true, HelloTimeout: 5 * time.Second})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// Conn.Receive reports the peer's close frame like the TS `ws` close event
// that SandboxChannel passes to onClose.
func TestConnReceive_ReportsPeerCloseCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frame  []byte
		code   int
		reason string
	}{
		{"status and reason", []byte{0x88, 0x05, 0x0f, 0xa0, 'b', 'y', 'e'}, 4000, "bye"},
		{"no status", []byte{0x88, 0x00}, 1005, ""},
		{"no frame", nil, 1006, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := bridge.Dial(context.Background(), rawCloseServer(t, tc.frame), bridge.DialOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_, err = conn.Receive()
			var ce *bridge.CloseError
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Reason != tc.reason {
				t.Fatalf("err = %#v, want code %d reason %q", err, tc.code, tc.reason)
			}
		})
	}
}

package websocket

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// V061 item 5: Node's WebSocket (undici websocket.js 541) fails the
// connection on a text frame that is not valid UTF-8: a close frame with
// status 1007 and reason "Received invalid UTF-8 in text frame.", then a
// socket error (TS onSocketError), not a message. A binary frame keeps the
// TextDecoder replacement (TestR84integFramesDecodedLikeTS).
func TestR86bintegInvalidUTF8TextFrameFailsWith1007(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	closePayload := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close() //nolint:errcheck
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		h := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		_, _ = io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+
			base64.StdEncoding.EncodeToString(h[:])+"\r\n\r\n")
		_, _ = c.Write([]byte{0x81, 0x03, 'a', 0xff, 'b'}) // text frame "a\xffb"
		var hdr [2]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0] != 0x88 {
			closePayload <- nil
			return
		}
		n := int(hdr[1] & 0x7f)
		var mask [4]byte
		_, _ = io.ReadFull(br, mask[:])
		p := make([]byte, n)
		_, _ = io.ReadFull(br, p)
		for i := range p {
			p[i] ^= mask[i%4]
		}
		closePayload <- p
	}()
	conn, err := Dial(context.Background(), "ws://"+ln.Addr().String()+"/", DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck
	msg, err := Receive(context.Background(), conn)
	if !errors.Is(err, ErrInvalidUTF8Text) || msg != "" || IsCleanClose(err) {
		t.Fatalf("Receive = %q, %v; want ErrInvalidUTF8Text (not a clean close)", msg, err)
	}
	select {
	case p := <-closePayload:
		if want := "\x03\xefReceived invalid UTF-8 in text frame."; string(p) != want {
			t.Errorf("close frame payload = %q, want status 1007 + undici reason", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no close frame")
	}
}

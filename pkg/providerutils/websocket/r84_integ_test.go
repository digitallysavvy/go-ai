package websocket

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	xws "golang.org/x/net/websocket"
)

// provb CR 1: TS readWebSocketMessageText (websocket.ts 49) gives a text
// frame's text without its leading BOM, and decodes a binary frame with
// TextDecoder (Node 24: EF BB BF 41 F0 9F 98 42 -> "A\uFFFDB").
func TestR84integFramesDecodedLikeTS(t *testing.T) {
	srv := httptest.NewServer(xws.Handler(func(c *xws.Conn) {
		_ = xws.Message.Send(c, "\uFEFF{}")
		_ = xws.Message.Send(c, []byte("\xEF\xBB\xBFA\xF0\x9F\x98B"))
		_ = xws.Message.Send(c, "plain")
	}))
	defer srv.Close()
	conn, err := Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	first, err := Receive(context.Background(), conn)
	if err != nil || first != "{}" {
		t.Fatalf("text frame = %q (%v)", first, err)
	}
	out := make(chan Message, 4)
	go ReceiveLoop(context.Background(), conn, out)
	if m := <-out; m.Err != nil || m.Text != "A\uFFFDB" {
		t.Fatalf("binary frame = %q (%v)", m.Text, m.Err)
	}
	if m := <-out; m.Err != nil || m.Text != "plain" {
		t.Fatalf("plain frame = %q (%v)", m.Text, m.Err)
	}
}

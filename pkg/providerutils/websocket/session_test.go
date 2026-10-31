package websocket

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// A connection registered after Close already ran must be closed, not leaked.
func TestSession_SetConnAfterCloseClosesConn(t *testing.T) {
	serverDone := make(chan struct{})
	ts := httptest.NewServer(websocket.Server{Handler: func(c *websocket.Conn) {
		defer close(serverDone)
		var s string
		_ = websocket.Message.Receive(c, &s) // returns once the client closes
	}})
	defer ts.Close()

	conn, err := Dial(context.Background(), "ws"+ts.URL[len("http"):], DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession[int](context.Background())
	_ = s.Close()
	s.SetConn(conn)

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("connection registered after Close was not closed")
	}
}

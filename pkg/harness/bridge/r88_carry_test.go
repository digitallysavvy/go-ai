package bridge

import (
	"context"
	"encoding/binary"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// r88carryReasonConn records how the channel closed it.
type r88carryReasonConn struct {
	in      chan []byte
	closed  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	reasons []string
	plain   int
}

func (c *r88carryReasonConn) Receive() ([]byte, error) {
	select {
	case m := <-c.in:
		return m, nil
	case <-c.closed:
		return nil, &CloseError{Code: 1000}
	}
}
func (c *r88carryReasonConn) Send([]byte) error { return nil }
func (c *r88carryReasonConn) Close() error {
	c.mu.Lock()
	c.plain++
	c.mu.Unlock()
	c.once.Do(func() { close(c.closed) })
	return nil
}
func (c *r88carryReasonConn) CloseWithReason(code int, reason string) error {
	c.mu.Lock()
	c.reasons = append(c.reasons, reason)
	c.mu.Unlock()
	c.once.Do(func() { close(c.closed) })
	return nil
}

// TS a54f0fd8c6 sandbox-channel.test.ts: suspend closes the socket with
// 1000 "suspended", so the bridge can tell a suspend from a drop.
func TestR88carrySuspendClosesWithReason(t *testing.T) {
	conn := &r88carryReasonConn{in: make(chan []byte), closed: make(chan struct{})}
	ch := NewChannel(ChannelOptions{Connect: func(context.Context) (Conn, error) { return conn, nil }})
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch.Suspend():
	case <-time.After(2 * time.Second):
		t.Fatal("suspend did not finish")
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.reasons) != 1 || conn.reasons[0] != "suspended" {
		t.Fatalf("close reasons = %v (plain closes %d)", conn.reasons, conn.plain)
	}
}

// A plain Close keeps closing without a reason (TS close()).
func TestR88carryCloseHasNoReason(t *testing.T) {
	conn := &r88carryReasonConn{in: make(chan []byte), closed: make(chan struct{})}
	ch := NewChannel(ChannelOptions{Connect: func(context.Context) (Conn, error) { return conn, nil }})
	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	ch.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !ch.IsClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.reasons) != 0 || conn.plain == 0 {
		t.Fatalf("reasons = %v, plain = %d", conn.reasons, conn.plain)
	}
}

// The WebSocket conn writes the close frame on the wire: status 1000 and
// the reason, read by the peer.
func TestR88carryWSConnCloseFrameOnTheWire(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		for {
			fr, err := ws.NewFrameReader()
			if err != nil {
				got <- "error: " + err.Error()
				return
			}
			payload, _ := io.ReadAll(fr)
			if fr.PayloadType() == websocket.CloseFrame {
				if len(payload) >= 2 {
					got <- string(payload[2:]) + "|" + strconv.Itoa(int(binary.BigEndian.Uint16(payload[:2])))
				} else {
					got <- "empty close"
				}
				return
			}
		}
	}))
	defer srv.Close()
	conn, err := Dial(context.Background(), harness.PortEndpoint{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rc, ok := conn.(reasonCloser)
	if !ok {
		t.Fatal("wsConn does not close with a reason")
	}
	if err := rc.CloseWithReason(1000, "suspended"); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Logf("close: %v", err)
	}
	select {
	case v := <-got:
		if v != "suspended|1000" {
			t.Fatalf("peer read %q", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no close frame")
	}
}

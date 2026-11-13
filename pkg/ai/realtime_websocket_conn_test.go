package ai

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// TestXNetWebSocketConn_ReceiveFrameReportsBinary verifies that
// xNetWebSocketConn.ReceiveFrame correctly distinguishes a binary WS frame
// from a text WS frame over a real WebSocket connection (audit row 8f89c25 /
// WG-MISC), and that SendBinary produces a binary frame the server sees as
// such (round-tripped through a raw echo handler using the frame's own
// payload type).
func TestXNetWebSocketConn_ReceiveFrameReportsBinary(t *testing.T) {
	handler := websocket.Handler(func(ws *websocket.Conn) {
		// Echo every frame back preserving its payload type: plain
		// Conn.Read/Write always send TextFrame (Conn.PayloadType's
		// default), so use the same type-preserving codec the client side
		// uses to both receive and re-send with the original opcode.
		for {
			var frame realtimeFrame
			if err := realtimeFrameTypeCodec.Receive(ws, &frame); err != nil {
				return
			}
			var sendErr error
			if frame.binary {
				sendErr = websocket.Message.Send(ws, frame.data)
			} else {
				sendErr = websocket.Message.Send(ws, string(frame.data))
			}
			if sendErr != nil {
				return
			}
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := WebSocketRealtimeDialer{}
	rawConn, err := dialer.Dial(context.Background(), provider.WebSocketConfig{URL: wsURL})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer rawConn.Close() //nolint:errcheck

	conn, ok := rawConn.(RealtimeBinaryConn)
	if !ok {
		t.Fatalf("dialed conn (%T) does not implement RealtimeBinaryConn", rawConn)
	}

	// Text frame round-trip: ReceiveFrame must report binary=false.
	if err := rawConn.Send(context.Background(), []byte(`{"type":"text-event"}`)); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	data, binary, err := conn.ReceiveFrame(context.Background())
	if err != nil {
		t.Fatalf("ReceiveFrame() (text) error = %v", err)
	}
	if binary {
		t.Error("expected binary=false for a text frame")
	}
	if string(data) != `{"type":"text-event"}` {
		t.Errorf("data = %q, want the echoed text event", data)
	}

	// Binary frame round-trip: ReceiveFrame must report binary=true.
	if err := conn.SendBinary(context.Background(), []byte{0x00, 0x01, 0x02, 0xff}); err != nil {
		t.Fatalf("SendBinary() error = %v", err)
	}
	data, binary, err = conn.ReceiveFrame(context.Background())
	if err != nil {
		t.Fatalf("ReceiveFrame() (binary) error = %v", err)
	}
	if !binary {
		t.Error("expected binary=true for a binary frame")
	}
	if len(data) != 4 || data[0] != 0x00 || data[3] != 0xff {
		t.Errorf("data = %v, want the echoed binary payload", data)
	}
}

// A receive abandoned by ctx cancellation must not swallow the next frame:
// the in-flight read is kept and its frame delivered to the next call.
func TestXNetWebSocketConn_CancelledReceiveDoesNotLoseFrame(t *testing.T) {
	handler := websocket.Handler(func(ws *websocket.Conn) {
		time.Sleep(150 * time.Millisecond)
		_ = websocket.Message.Send(ws, "late-1")
		_ = websocket.Message.Send(ws, "late-2")
		time.Sleep(500 * time.Millisecond)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	rawConn, err := WebSocketRealtimeDialer{}.Dial(context.Background(), provider.WebSocketConfig{URL: "ws" + strings.TrimPrefix(server.URL, "http")})
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close() //nolint:errcheck

	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := rawConn.Receive(short); err == nil {
		t.Fatal("first Receive should time out")
	}
	for _, want := range []string{"late-1", "late-2"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		got, err := rawConn.Receive(ctx)
		cancel()
		if err != nil || string(got) != want {
			t.Fatalf("Receive = %q, %v; want %q", got, err, want)
		}
	}
}

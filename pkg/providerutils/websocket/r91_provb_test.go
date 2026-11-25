package websocket

import (
	"context"
	"testing"
)

// core2-02 (websocket.go site): Receive and ReceiveLoop read whole messages
// (TS websocket.ts:56 gets the reassembled event.data): a fragmented text
// message is one message, and UTF-8 split across fragments is valid.
func TestR91provbReceiveWholeMessages(t *testing.T) {
	eacute := []byte("é") // c3 a9
	frames := [][]byte{
		r91frame(false, opText, []byte(`{"a`)),
		r91frame(true, opContinuation, []byte(`"}`)),
		r91frame(false, opText, append([]byte("caf"), eacute[:1]...)),
		r91frame(true, opContinuation, eacute[1:]),
		closeFrame(1000, ""),
	}
	t.Run("Receive", func(t *testing.T) {
		conn := r91dial(t, rawCloseServer(t, frames...))
		for _, want := range []string{`{"a"}`, "café"} {
			got, err := Receive(context.Background(), conn)
			if err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		}
	})
	t.Run("ReceiveLoop", func(t *testing.T) {
		conn := r91dial(t, rawCloseServer(t, frames...))
		out := make(chan Message, 4)
		go ReceiveLoop(context.Background(), conn, out)
		for _, want := range []string{`{"a"}`, "café"} {
			if m := <-out; m.Err != nil || m.Text != want {
				t.Fatalf("got %q, %v; want %q", m.Text, m.Err, want)
			}
		}
		if m := <-out; !IsCleanClose(m.Err) {
			t.Fatalf("end = %v, want a clean close", m.Err)
		}
	})
}

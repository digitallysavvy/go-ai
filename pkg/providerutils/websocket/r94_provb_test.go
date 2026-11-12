package websocket

import (
	"context"
	"errors"
	"io"
	"testing"
)

// TestR94provbSessionEmitAbortCause (cr-r94-provg-to-provb, cr-r94-prova-to-provb):
// TS connectToWebSocket fails the stream with abortSignal.reason, so a part
// still waiting for the consumer when the caller cancels ends the stream with
// the cancel cause, not a clean io.EOF; after Close the stream just ends.
func TestR94provbSessionEmitAbortCause(t *testing.T) {
	cause := errors.New("r94provb caller cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	s := NewSession[int](ctx)
	cancel(cause)
	func() {
		defer s.CloseParts()
		defer s.CancelContext()
		if s.Emit(1) {
			t.Error("Emit succeeded on a cancelled session")
		}
	}()
	if _, err := s.Next(); !errors.Is(err, cause) {
		t.Fatalf("Next err = %v, want the cancel cause", err)
	}

	closed := NewSession[int](context.Background())
	_ = closed.Close()
	func() {
		defer closed.CloseParts()
		closed.Emit(1)
	}()
	if _, err := closed.Next(); err != io.EOF {
		t.Fatalf("after Close: err = %v, want io.EOF", err)
	}
}

// TestR94provbToWebSocketURL: TS toWebSocketUrl is new URL(url) with the
// protocol switched, so the host is lower-cased, a default port is left out
// and an unparsable URL is the TypeError "Invalid URL".
func TestR94provbToWebSocketURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://API.Example.com:443/v1/stt":                  "wss://api.example.com/v1/stt",
		"http://localhost:80/x?a=1":                           "ws://localhost/x?a=1",
		"http://localhost:8080/proxy/https://api.x.ai/v1/stt": "ws://localhost:8080/proxy/https://api.x.ai/v1/stt",
		"wss://h/p": "wss://h/p",
	} {
		if got, err := ToWebSocketURL(in); err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ToWebSocketURL("http://[::1"); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid: err = %v", err)
	}
}

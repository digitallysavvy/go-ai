package gateway

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// A cancelled context stops the reader and the text pump: nothing is left
// running.
func TestSpeechModel_DoStreamSpeech_ContextCancelLeavesNoGoroutines(t *testing.T) {
	handshaken := make(chan struct{}, 1)
	ts := httptest.NewServer(websocket.Server{Handshake: func(cfg *websocket.Config, _ *stdhttp.Request) error {
		// the handshake needs exactly one echoed subprotocol
		if len(cfg.Protocol) > 0 {
			cfg.Protocol = cfg.Protocol[:1]
		}
		return nil
	}, Handler: func(c *websocket.Conn) {
		handshaken <- struct{}{}
		for { // silent server
			var s string
			if websocket.Message.Receive(c, &s) != nil {
				return
			}
		}
	}})
	defer ts.Close()
	base := runtime.NumGoroutine()

	text := texts("Hi")
	text.block = true
	ctx, cancel := context.WithCancel(context.Background())
	result, err := newTestGatewaySpeechModel(t, ts.URL).DoStreamSpeech(ctx, &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handshaken:
	case <-time.After(2 * time.Second):
		t.Fatal("no connection")
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	for {
		if _, err := result.Stream.Next(); err != nil {
			break
		}
	}
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text was not cancelled")
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines = %d, want <= %d\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A server that never completes the WebSocket handshake cannot hang the
// caller past its context.
func TestSpeechModel_DoStreamSpeech_StalledHandshakeHonorsContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() //nolint:errcheck
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	text := texts("Hi")
	result, err := newTestGatewaySpeechModel(t, "http://"+ln.Addr().String()).DoStreamSpeech(ctx, &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := result.Stream.Next(); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream hung on a stalled handshake")
	}
	select {
	case <-text.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("text was not cancelled")
	}
}

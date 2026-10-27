package elevenlabs

import (
	"context"
	"net"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// waitGoroutines polls until the goroutine count is back to at most base.
func waitGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines = %d, want <= %d\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A cancelled context stops the reader, the writer (text pump, keepalive
// timer) and the dial: nothing is left running.
func TestSpeechStream_ContextCancelLeavesNoGoroutines(t *testing.T) {
	old := speechKeepaliveInterval
	speechKeepaliveInterval = 20 * time.Millisecond
	defer func() { speechKeepaliveInterval = old }()

	handshaken := make(chan struct{}, 1)
	ts := httptest.NewServer(websocket.Server{Handler: func(c *websocket.Conn) {
		handshaken <- struct{}{}
		for { // silent server: reads (keepalives included) and never answers
			var s string
			if websocket.Message.Receive(c, &s) != nil {
				return
			}
		}
	}})
	defer ts.Close()
	base := runtime.NumGoroutine()

	text := speechTexts()
	text.block = true
	ctx, cancel := context.WithCancel(context.Background())
	result, err := newSpeechTestModel(t, ts.URL, "eleven_flash_v2_5").DoStreamSpeech(ctx, &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handshaken:
	case <-time.After(2 * time.Second):
		t.Fatal("no connection")
	}
	time.Sleep(100 * time.Millisecond) // several keepalives
	cancel()
	for {
		if _, err := result.Stream.Next(); err != nil {
			break
		}
	}
	waitGoroutines(t, base)
}

// A server that accepts the TCP connection but never completes the WebSocket
// handshake cannot hang the caller past its context.
func TestSpeechStream_StalledHandshakeHonorsContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
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
	text := speechTexts("Hi")
	result, err := newSpeechTestModel(t, "http://"+ln.Addr().String(), "eleven_flash_v2_5").DoStreamSpeech(ctx, &provider.SpeechStreamOptions{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := collectSpeech(t, result.Stream); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream hung on a stalled handshake")
	}
	if !text.wasCancelled() {
		t.Fatal("text was not cancelled")
	}
}

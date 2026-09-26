package bridge_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// Dial tests against a real WebSocket server (bridgetest.Server), covering
// the hello-race fix (5cc654f), the startup-timeout cleanup fix (ab46c30),
// and abort semantics (d975097). Package bridge_test because bridgetest
// imports bridge.

// TS/5cc654f: an immediate bridge-hello (sent the instant the bridge accepts
// the connection) must never be missed, because Dial reads synchronously
// from the socket before returning.
func TestDialHelloRace(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)

	var hello *bridge.Hello
	conn, err := bridge.Dial(context.Background(), srv.Endpoint(), bridge.DialOptions{
		WaitForHello: true,
		HelloTimeout: time.Second,
		OnHello:      func(h *bridge.Hello) { hello = h },
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if hello == nil {
		t.Fatal("OnHello was never called")
	}
	if hello.State != "waiting" {
		t.Fatalf("hello.State = %q, want %q", hello.State, "waiting")
	}
	if !hello.SupportsUserMessageResponses() {
		t.Fatal("expected experimental_userMessageResponses capability")
	}
}

// A bridge that never sends bridge-hello must time out with a descriptive
// error rather than hang.
func TestDialHelloTimeout(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", SkipHello: true})
	t.Cleanup(srv.Close)

	_, err := bridge.Dial(context.Background(), srv.Endpoint(), bridge.DialOptions{
		Name:         "test bridge",
		WaitForHello: true,
		HelloTimeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "did not send bridge-hello") {
		t.Fatalf("err = %v, want it to mention \"did not send bridge-hello\"", err)
	}
}

// ab46c30: canceling the context mid-wait for bridge-hello must return
// promptly (no hang), close the socket, and not leak the goroutine blocked on
// the read.
func TestDialStartupTimeoutCleanup(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", SkipHello: true})
	t.Cleanup(srv.Close)

	before := goroutineCountSettled()

	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		start := time.Now()
		_, err := bridge.Dial(ctx, srv.Endpoint(), bridge.DialOptions{
			Name:         "test bridge",
			WaitForHello: true,
			HelloTimeout: 5 * time.Second, // long: the ctx timeout must win
		})
		elapsed := time.Since(start)
		cancel()
		if err == nil {
			t.Fatal("expected an error when the context is canceled mid-wait")
		}
		if elapsed > time.Second {
			t.Fatalf("Dial took %s to return after context cancellation, want well under 1s", elapsed)
		}
	}

	after := goroutineCountSettled()
	// Generous slack: httptest/ws server internals may retain a couple of
	// goroutines transiently, but 20 leaked read-loop goroutines (one per
	// Dial call above) would show up unmistakably.
	if after > before+10 {
		t.Fatalf("goroutine count grew from %d to %d after 20 canceled dials — possible leak", before, after)
	}
}

// d975097: an already-canceled context must abort immediately without
// attempting the dial.
func TestDialAbortAlreadyCanceled(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := bridge.Dial(ctx, srv.Endpoint(), bridge.DialOptions{WaitForHello: true, HelloTimeout: time.Second})
	if err == nil {
		t.Fatal("expected an abort error for an already-canceled context")
	}
}

// d975097: canceling the context while waiting for hello aborts the dial
// (a slow-hello bridge cannot block Open() forever).
func TestDialAbortDuringHelloWait(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", HelloDelay: 2 * time.Second})
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := bridge.Dial(ctx, srv.Endpoint(), bridge.DialOptions{WaitForHello: true, HelloTimeout: 5 * time.Second})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an abort error")
		}
	case <-time.After(time.Second):
		t.Fatal("Dial did not return promptly after cancellation")
	}
}

func goroutineCountSettled() int {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	return runtime.NumGoroutine()
}

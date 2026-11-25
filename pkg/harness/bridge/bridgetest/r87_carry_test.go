package bridgetest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// Like runBridge at TS 3bf70f83bc (harness/src/bridge/index.ts 726-731,
// 1013-1018): the fake bridge advertises idempotentStart and drops a start
// whose startId repeats the last one; starts without a startId always run.
func TestR87carryFakeBridgeDropsRepeatedStartID(t *testing.T) {
	var starts atomic.Int32
	srv := NewServer(Options{Token: "tok", OnStart: func(*Turn, map[string]any) { starts.Add(1) }})
	defer srv.Close()
	var hello atomic.Pointer[bridge.Hello]
	conn, err := bridge.Dial(context.Background(), srv.Endpoint(), bridge.DialOptions{WaitForHello: true, HelloTimeout: 2 * time.Second, OnHello: func(h *bridge.Hello) { hello.Store(h) }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if !hello.Load().SupportsIdempotentStart() {
		t.Fatal("the fake bridge does not advertise idempotentStart")
	}
	for _, id := range []string{"a", "a", "b", "", ""} {
		data, _ := bridge.MarshalInbound(bridge.StartBase{Prompt: "x", StartID: id})
		if err := conn.Send(data); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for starts.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := starts.Load(); got != 4 {
		t.Fatalf("starts run = %d, want 4", got)
	}
}

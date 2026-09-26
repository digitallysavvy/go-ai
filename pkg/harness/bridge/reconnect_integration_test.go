package bridge_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// Port of harness/src/bridge/reconnect.integration.test.ts: the real
// host-side Channel talking to a real WebSocket server (bridgetest.Server
// standing in for the TS in-sandbox runBridge) over a loopback socket. A
// mid-turn drop must be invisible to the consumer, and a suspend/resume must
// hand off the tail to a fresh channel with no gap and no duplicate.

func textDelta(e bridge.Event) (string, bool) {
	f, ok := e.Message.(bridge.StreamPartFrame)
	if !ok {
		return "", false
	}
	p, ok := f.Part.(*harness.TextDeltaPart)
	if !ok {
		return "", false
	}
	return p.Delta, true
}

func dialConnect(endpoint harness.PortEndpoint) bridge.ConnectFunc {
	return bridge.NewConnectFunc(endpoint, bridge.DialOptions{WaitForHello: true, HelloTimeout: 2 * time.Second})
}

// TS: "transparently reconnects mid-turn and delivers every event in order, once"
func TestReconnectIntegrationTransientDrop(t *testing.T) {
	release := make(chan struct{})
	srv := bridgetest.NewServer(bridgetest.Options{
		Token: "integ-token",
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "one"})   // seq 1
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "two"})   // seq 2
			<-release                                                                    // socket is dropped while we wait here
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "three"}) // seq 3
			turn.Emit(map[string]any{
				"type":         "finish",
				"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
				"totalUsage": map[string]any{
					"inputTokens":  map[string]any{"total": 1},
					"outputTokens": map[string]any{"total": 2},
				},
			}) // seq 4
		},
	})
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	var debug []string
	var deltas []string
	var finishes int
	var closed bool
	var closedCode int

	ch := bridge.NewChannel(bridge.ChannelOptions{
		Connect:   dialConnect(srv.Endpoint()),
		Reconnect: bridge.ReconnectOptions{InitialDelay: 5 * time.Millisecond, MaxDelay: 20 * time.Millisecond, MaxElapsed: 2 * time.Second},
		OnDebug: func(e bridge.ChannelDebugEvent) {
			mu.Lock()
			debug = append(debug, e.Event)
			mu.Unlock()
		},
	})
	t.Cleanup(ch.Close)

	ch.On("text-delta", func(e bridge.Event) {
		delta, ok := textDelta(e)
		if !ok {
			t.Errorf("unexpected message type %T", e.Message)
			return
		}
		mu.Lock()
		deltas = append(deltas, delta)
		mu.Unlock()
	})
	finished := make(chan struct{})
	ch.On("finish", func(bridge.Event) {
		mu.Lock()
		finishes++
		mu.Unlock()
		close(finished)
	})
	ch.OnClose(func(code int, _ string) {
		mu.Lock()
		closed, closedCode = true, code
		mu.Unlock()
	})

	if err := ch.Open(context.Background(), false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := ch.Send(bridge.StartBase{Prompt: "hi"}); err != nil {
		t.Fatalf("Send(start): %v", err)
	}

	waitForT(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(deltas) == 2
	})
	mu.Lock()
	if got := append([]string(nil), deltas...); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		mu.Unlock()
		t.Fatalf("deltas before drop = %v, want [one two]", got)
	}
	mu.Unlock()

	// Force an abrupt drop of the live socket and wait for the channel to
	// reconnect *before* the bridge emits anything more, so the post-drop
	// events can only have arrived over the replacement socket.
	srv.DropActive()
	waitForT(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range debug {
			if e == bridge.DebugReconnected {
				return true
			}
		}
		return false
	})

	// Now let the bridge finish the turn over the reconnected socket.
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not finish over the reconnected socket")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"one", "two", "three"}
	if len(deltas) != len(want) {
		t.Fatalf("deltas = %v, want %v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Fatalf("deltas = %v, want %v", deltas, want)
		}
	}
	if finishes != 1 {
		t.Fatalf("finishes = %d, want 1", finishes)
	}
	if closed {
		t.Fatalf("OnClose fired with code %d — a transient drop must never surface as a close", closedCode)
	}
}

// TS: "suspends mid-turn, then a fresh-process channel attaches and continues exactly once"
func TestReconnectIntegrationSuspendAndResume(t *testing.T) {
	release := make(chan struct{})
	srv := bridgetest.NewServer(bridgetest.Options{
		Token: "integ-token",
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "one"})   // seq 1
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "two"})   // seq 2
			<-release                                                                    // suspended here — no active socket
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "three"}) // seq 3
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "four"})  // seq 4
			turn.Emit(map[string]any{
				"type":         "finish",
				"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
				"totalUsage": map[string]any{
					"inputTokens":  map[string]any{"total": 1},
					"outputTokens": map[string]any{"total": 2},
				},
			}) // seq 5
		},
	})
	t.Cleanup(srv.Close)

	// ── Slice 1: open, start, receive the first two deltas, then suspend. ──
	chA := bridge.NewChannel(bridge.ChannelOptions{Connect: dialConnect(srv.Endpoint())})
	t.Cleanup(chA.Close)
	var muA sync.Mutex
	var deltasA []string
	chA.On("text-delta", func(e bridge.Event) {
		delta, ok := textDelta(e)
		if !ok {
			return
		}
		muA.Lock()
		deltasA = append(deltasA, delta)
		muA.Unlock()
	})
	var suspendedReason string
	chA.OnClose(func(_ int, reason string) { suspendedReason = reason })

	if err := chA.Open(context.Background(), false); err != nil {
		t.Fatalf("chA.Open: %v", err)
	}
	if err := chA.Send(bridge.StartBase{Prompt: "hi"}); err != nil {
		t.Fatalf("chA.Send(start): %v", err)
	}
	waitForT(t, 2*time.Second, func() bool {
		muA.Lock()
		defer muA.Unlock()
		return len(deltasA) == 2
	})

	cursor := <-chA.Suspend()
	muA.Lock()
	gotA := append([]string(nil), deltasA...)
	muA.Unlock()
	if len(gotA) != 2 || gotA[0] != "one" || gotA[1] != "two" {
		t.Fatalf("deltasA = %v, want [one two]", gotA)
	}
	if cursor != 2 {
		t.Fatalf("cursor = %v, want 2 (froze exactly at the last delivered event)", cursor)
	}
	if suspendedReason != bridge.CloseReasonSuspended {
		t.Fatalf("suspendedReason = %q, want %q", suspendedReason, bridge.CloseReasonSuspended)
	}

	// The turn keeps running in the bridge after the host went away.
	close(release)

	// ── Slice 2: a fresh channel attaches from the persisted cursor. ──
	chB := bridge.NewChannel(bridge.ChannelOptions{
		Connect:                dialConnect(srv.Endpoint()),
		InitialLastSeenEventID: cursor,
	})
	t.Cleanup(chB.Close)
	var muB sync.Mutex
	var deltasB []string
	chB.On("text-delta", func(e bridge.Event) {
		delta, ok := textDelta(e)
		if !ok {
			return
		}
		muB.Lock()
		deltasB = append(deltasB, delta)
		muB.Unlock()
	})
	finishedB := make(chan struct{})
	chB.On("finish", func(bridge.Event) { close(finishedB) })

	if err := chB.Open(context.Background(), true); err != nil {
		t.Fatalf("chB.Open(resume): %v", err)
	}
	select {
	case <-finishedB:
	case <-time.After(2 * time.Second):
		t.Fatal("slice 2 did not observe the turn finish")
	}

	muB.Lock()
	defer muB.Unlock()
	// No gap (got the whole tail) and no duplicate (none of slice 1's events).
	if len(deltasB) != 2 || deltasB[0] != "three" || deltasB[1] != "four" {
		t.Fatalf("deltasB = %v, want [three four]", deltasB)
	}
}

func waitForT(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

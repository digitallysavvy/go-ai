package claudecode

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// TS 3bf70f83bc: "cleans up a turn whose initial start cannot be sent". The
// send error settles the turn, stops the deadline and removes the listeners.
func TestR87carryInitialStartSendErrorCleansUp(t *testing.T) {
	ch := bridge.NewChannel(bridge.ChannelOptions{})
	ch.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !ch.IsClosed() {
		if time.Now().After(deadline) {
			t.Fatal("channel did not close")
		}
		time.Sleep(time.Millisecond)
	}
	s := newSession(sessionOptions{sessionID: "s1", channel: ch, turnStartTimeout: 50 * time.Millisecond})
	pc := s.wireTurn(context.Background(), func(harness.StreamPart) {})
	err := pc.sendStart(&StartFrame{})
	if err == nil || err.Error() != "SandboxChannel: cannot send start — channel is closed." {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-pc.Done():
	default:
		t.Fatal("the turn did not settle")
	}
	pc.mu.Lock()
	timer, unsubs, pending := pc.startTimer, pc.unsubs, pc.pendingStart
	pc.mu.Unlock()
	if timer != nil || unsubs != nil || pending != nil {
		t.Fatalf("timer = %v, unsubs = %d, pending = %v", timer, len(unsubs), pending)
	}
	if pc.Err() != err {
		t.Fatalf("Err = %v, want the send error", pc.Err())
	}
}

// Ruling 5: zero is the TS default (120 s), a negative value disables it.
func TestR87carryTurnStartTimeoutDefault(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{0: 120 * time.Second, -time.Second: 0, 5 * time.Second: 5 * time.Second} {
		if got := turnStartTimeout(Settings{TurnStartTimeout: in}); got != want {
			t.Errorf("turnStartTimeout(%v) = %v, want %v", in, got, want)
		}
	}
}

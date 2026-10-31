package claudecode_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

type debugRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *debugRecorder) record(ev bridge.ChannelDebugEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev.Event)
	r.mu.Unlock()
}

func (r *debugRecorder) has(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e == name {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TS #24065: `onDebug` is forwarded to both the fresh and the attached
// channel, so a host that caches idle sessions sees the bridge connection
// closing for good.
func TestSettingsOnDebug_ForwardedToFreshAndAttachedChannels(t *testing.T) {
	for _, attach := range []bool{false, true} {
		name := "fresh"
		if attach {
			name = "attached"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{}, 1)
			srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(*bridgetest.Turn, map[string]any) {
				select {
				case started <- struct{}{}:
				default:
				}
			}})
			t.Cleanup(srv.Close)
			sandbox := newTestSandbox(srv)
			wireSpawn(sandbox)

			rec := &debugRecorder{}
			newHarness := func(onDebug func(bridge.ChannelDebugEvent)) *claudecode.Harness {
				h, err := claudecode.New(claudecode.Settings{
					StartupTimeout:  2 * time.Second,
					MintBridgeToken: func(string) string { return "tok" },
					Reconnect:       bridge.ReconnectOptions{MaxElapsed: 30 * time.Millisecond, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
					OnDebug:         onDebug,
				})
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return h
			}
			opts := harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"}

			var sess harness.Session
			if attach {
				first, err := newHarness(nil).DoStart(context.Background(), opts)
				if err != nil {
					t.Fatalf("DoStart: %v", err)
				}
				opts.ResumeFrom, err = first.DoDetach(context.Background())
				if err != nil {
					t.Fatalf("DoDetach: %v", err)
				}
			}
			sess, err := newHarness(rec.record).DoStart(context.Background(), opts)
			if err != nil {
				t.Fatalf("DoStart: %v", err)
			}
			t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
			// A sent start makes this the active connection the server can drop.
			if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
				Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
			}); err != nil {
				t.Fatalf("DoPromptTurn: %v", err)
			}

			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("bridge never received the start frame")
			}

			// The bridge goes away for good: reconnect attempts fail and the
			// channel reports `reconnect-failed` to the callback.
			srv.DropActive()
			srv.Close()
			waitFor(t, "reconnect-failed", func() bool { return rec.has(bridge.DebugReconnectFailed) })
			if !rec.has(bridge.DebugReconnectAttempt) {
				t.Error("no reconnect-attempt reported")
			}
		})
	}
}

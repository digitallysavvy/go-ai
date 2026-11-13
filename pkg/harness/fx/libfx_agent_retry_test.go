package fx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
)

func drainStream(t *testing.T, r *ai.StreamTextResult) {
	t.Helper()
	stream := r.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			return
		}
	}
}

// Port of TS fx-harness.test.ts "allows an immediate HarnessAgent retry
// while %s cleanup is pending": the failed turn's stream settles while the
// runtime is still closing, and the retry starts its runtime only once that
// close has finished.
func TestLibfxAgentRetryWhileCleanupPending(t *testing.T) {
	for _, outcome := range []string{"error", "abort"} {
		t.Run(outcome, func(t *testing.T) {
			h, m := newMockHarness(t, Settings{})
			failure := errors.New("First turn failed")
			release := make(chan struct{})
			var releaseOnce func()
			{
				done := false
				releaseOnce = func() {
					if !done {
						done = true
						close(release)
					}
				}
			}
			defer releaseOnce()
			closeStarted := newSignal()
			firstCtx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			m.mu.Lock()
			m.close = func() error {
				closeStarted.fire()
				<-release
				return nil
			}
			m.run = func(context.Context, runtimeOptions) error {
				if outcome == "abort" {
					cancel(failure)
					return nil
				}
				return failure
			}
			m.mu.Unlock()
			a, err := harness.NewAgent(harness.AgentSettings{Harness: h})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			session, err := a.CreateSession(ctx, harness.CreateSessionOptions{SandboxSession: newFakeSandbox()})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				releaseOnce()
				_ = session.Destroy(ctx)
			}()
			first, err := a.Stream(firstCtx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "fail", HarnessSession: session}})
			if err != nil {
				t.Fatal(err)
			}
			within(t, 10*time.Second, "the failed stream", func() { drainStream(t, first) })
			// An abort settles through the harness's abort chunk, whose
			// error carries the caller's cause as text.
			if err := first.Err(); err == nil || (!errors.Is(err, failure) && !strings.Contains(err.Error(), failure.Error())) {
				t.Errorf("first turn error = %v, want %v", err, failure)
			}
			if session.HasUnfinishedTurn() {
				t.Error("failed turn left an unfinished turn")
			}
			closeStarted.wait(t)
			m.mu.Lock()
			m.run, m.close = nil, nil
			m.mu.Unlock()
			type streamed struct {
				r   *ai.StreamTextResult
				err error
			}
			retry := make(chan streamed, 1)
			go func() {
				r, err := a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "retry", HarnessSession: session}})
				retry <- streamed{r, err}
			}()
			time.Sleep(100 * time.Millisecond)
			if n := len(m.opts()); n != 1 {
				t.Errorf("runtimes started while cleanup was pending = %d", n)
			}
			releaseOnce()
			var second streamed
			select {
			case second = <-retry:
			case <-time.After(10 * time.Second):
				t.Fatal("retry did not start")
			}
			if second.err != nil {
				t.Fatal(second.err)
			}
			within(t, 10*time.Second, "the retry stream", func() { drainStream(t, second.r) })
			if err := second.r.Err(); err != nil {
				t.Errorf("retry error = %v", err)
			}
			if n := len(m.opts()); n != 2 {
				t.Errorf("runtimes = %d, want 2", n)
			}
		})
	}
}

package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A turn the host suspends before the adapter sent `finish` still ends its
// stream with a finish part: TS runPrompt calls `result.finish(undefined)`
// in the isTurnSuspending branch, which enqueues `finish` with the last
// step's finish reason ('other' before any step) after discarding the
// unfinished step.
func TestR49SuspendedTurnWithoutFinishStillEmitsFinish(t *testing.T) {
	finish := make(chan struct{})
	suspendCalled := make(chan struct{})
	var once sync.Once
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &TextDeltaPart{ID: "t", Delta: "partial"}}
		},
		promptDone: func() <-chan struct{} {
			select {
			case <-finish:
				return nil
			default:
				return finish
			}
		},
		doSuspendTurn: func(context.Context) (*ContinueTurnState, error) {
			once.Do(func() { close(suspendCalled); close(finish) })
			return NewContinueTurnState("mock", map[string]any{})
		},
	})
	a, session := newTestAgent(t, mock, map[string]types.Tool{}, Callbacks{}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.SuspendTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-suspendCalled
	var finishes []provider.StreamChunk
	stream := res.Stream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes = append(finishes, *c)
		}
	}
	if len(finishes) != 1 || finishes[0].FinishReason != types.FinishReason("other") {
		t.Fatalf("finish chunks = %+v, want one with finish reason other", finishes)
	}
	if err := res.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if got := res.FinishReason(); got != types.FinishReason("other") {
		t.Fatalf("FinishReason() = %q, want other", got)
	}
	if len(res.Steps()) != 0 {
		t.Fatalf("steps = %d, want 0 (the unfinished step is discarded)", len(res.Steps()))
	}
}

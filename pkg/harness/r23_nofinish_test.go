package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS runPrompt: a harness stream that ends without a terminal `finish`
// reports onTurnFailed and then `result.finish(undefined)`, so the result
// settles normally (no error) with the last step's finish reason.
func TestRunPrompt_StreamEndingWithoutFinishSettlesNormally(t *testing.T) {
	mock := erroringHarness([]StreamPart{
		&StreamStartPart{},
		&TextDeltaPart{ID: "t1", Delta: "hi"},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
	}, nil)
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	res, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate err = %v, want nil", err)
	}
	if res.Text != "hi" {
		t.Fatalf("Text = %q", res.Text)
	}
	if string(res.FinishReason) != "stop" {
		t.Fatalf("FinishReason = %q, want stop", res.FinishReason)
	}
	// the session returned to idle (onTurnFailed ran): a second turn starts.
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "again", HarnessSession: session}); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
}

// With unclosed step content the TS result fails.
func TestRunPrompt_StreamEndingWithoutFinishAndOpenStepFails(t *testing.T) {
	mock := erroringHarness([]StreamPart{
		&StreamStartPart{},
		&TextDeltaPart{ID: "t1", Delta: "hi"},
	}, nil)
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	_, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session})
	if err == nil {
		t.Fatal("want unclosed step error")
	}
}

// TS calls lifecycle.end when the terminal `finish` arrives, before
// result.finish() rejects unclosed step content, so a turn with a completed
// step ends (OnEnd fires once) even though the result fails.
func TestRunPrompt_FinishWithUnclosedStepAfterCompletedStepStillEndsLifecycle(t *testing.T) {
	mock := erroringHarness([]StreamPart{
		&StreamStartPart{},
		&TextDeltaPart{ID: "t1", Delta: "one"},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&TextDeltaPart{ID: "t2", Delta: "unflushed"},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
	}, nil)
	var ends int
	a, session := newTestAgent(t, mock, nil, Callbacks{OnEnd: func(_ context.Context, e ai.OnFinishEvent) {
		ends++
		if len(e.Steps) != 1 {
			t.Errorf("OnEnd steps = %d, want 1", len(e.Steps))
		}
	}}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err == nil || err.Error() != unclosedStepErrorMessage {
		t.Fatalf("Err() = %v, want %q", err, unclosedStepErrorMessage)
	}
	if ends != 1 {
		t.Fatalf("OnEnd called %d times, want 1", ends)
	}
}

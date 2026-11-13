package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
)

// TS finish(undefined) with no recorded step: steps stays empty (the
// synthetic final step is not appended to the steps buffer) and the finish
// reason is `other`.
func TestRunPrompt_NoFinishNoStepsLeavesStepsEmpty(t *testing.T) {
	mock := erroringHarness([]StreamPart{&StreamStartPart{}}, nil)
	a, session := newTestAgent(t, mock, nil, Callbacks{}, nil)
	res, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session})
	if err != nil {
		t.Fatalf("Generate err = %v", err)
	}
	if string(res.FinishReason) != "other" {
		t.Fatalf("FinishReason = %q, want other", res.FinishReason)
	}
	if len(res.Steps) != 0 {
		t.Fatalf("Steps = %d, want 0", len(res.Steps))
	}
}

package codex

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// TS doPromptTurn builds the start message (consuming the seeded resume thread
// id) and clears the recovery flag even when the abort signal already fired and
// sendStart skips the frame.
func TestDoPromptTurnAbortedStillConsumesSeededResumeThread(t *testing.T) {
	ch := bridge.NewChannel(bridge.ChannelOptions{})
	s := newSession(sessionOptions{
		channel: ch, sandboxHomeDir: "/home/agent", sandbox: bridgetest.NewSandbox(),
		resumeThreadID: "thread-1", seedResumeThreadOnFirstPrompt: true, rerunContinue: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{
		Prompt:       harness.TextPrompt("hi"),
		TurnSettings: harness.TurnSettings{},
		Emit:         func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingResumeThreadID != "" {
		t.Fatalf("pendingResumeThreadID = %q, want consumed", s.pendingResumeThreadID)
	}
	if s.rerunContinue {
		t.Fatal("rerunContinue was not cleared")
	}
}

package acp

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// A turn that settled early on a callback error is still finished by the
// bridge after its abort; TS's session-level markTurnFinished listeners clear
// the in-flight flag then, so the session can start its next prompt.
func TestTurnInFlightClearsWhenBridgeFinishesAfterEarlySettle(t *testing.T) {
	const token = "late-finish-token"
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{
			"type": "acp-tool-call-candidate", "requestId": "req-1",
			"toolCall": map[string]any{"toolCallId": "tc-1", "title": "Run"},
		})
		turn.Emit(map[string]any{"type": "tool-call", "toolCallId": "tc-1", "toolName": "run", "input": "{}"})
		time.Sleep(50 * time.Millisecond)
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
		})
	})
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		s.IsMcpToolCall = func(ToolCall) bool { panic("classifier failed") }
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv),
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi")})
	if err != nil {
		t.Fatal(err)
	}
	<-control.Done()
	s := sess.(*session)
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		inFlight := s.turnInFlight
		s.mu.Unlock()
		if !inFlight {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("turnInFlight stayed true after the bridge finished the aborted turn")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

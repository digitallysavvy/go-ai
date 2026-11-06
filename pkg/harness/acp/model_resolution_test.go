package acp

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// runPromptTurn runs one text turn against a bridge that answers with the
// given stream-start frame, returning the start frame and the emitted parts.
func runPromptTurn(t *testing.T, mut func(*Settings), streamStart map[string]any, model string) (map[string]any, []harness.StreamPart) {
	t.Helper()
	const token = "model-token"
	got := make(chan map[string]any, 1)
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		got <- start
		turn.Emit(streamStart)
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		if mut != nil {
			mut(s)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "model-session", SessionWorkDir: "/vercel/sandbox/test-acp-model", SandboxSession: newFakeSandbox(srv),
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	var mu sync.Mutex
	var parts []harness.StreamPart
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Model: model},
		Prompt:       harness.TextPrompt("hi"),
		Emit: func(p harness.StreamPart) {
			mu.Lock()
			parts = append(parts, p)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	var start map[string]any
	select {
	case start = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	return start, append([]harness.StreamPart(nil), parts...)
}

// TS #23940: the bridge always receives the model mapping, even without a
// model, so it can read the resolved model from the session state.
func TestStartFrameAlwaysCarriesModelMapping(t *testing.T) {
	start, _ := runPromptTurn(t, nil, map[string]any{"type": "stream-start"}, "")
	if _, hasModel := start["model"]; hasModel {
		t.Fatalf("start.model = %v, want it omitted", start["model"])
	}
	if want := map[string]any{"type": "session-model", "path": "model"}; !reflect.DeepEqual(start["modelMapping"], want) {
		t.Fatalf("start.modelMapping = %#v, want %#v", start["modelMapping"], want)
	}
}

// TS #23940 (GitHub Copilot): a stream-start part without a model gets the
// configured fallback; one that reports a model is left alone.
func TestStreamStartFallbackModel(t *testing.T) {
	withFallback := func(s *Settings) { s.StreamStartFallbackModelID = "openai/gpt-5.5" }
	streamStartModel := func(parts []harness.StreamPart) string {
		for _, p := range parts {
			if s, ok := p.(*harness.StreamStartPart); ok {
				return s.ModelID
			}
		}
		t.Fatal("no stream-start part")
		return ""
	}
	_, parts := runPromptTurn(t, withFallback, map[string]any{"type": "stream-start"}, "")
	if got := streamStartModel(parts); got != "openai/gpt-5.5" {
		t.Fatalf("modelId = %q, want the fallback", got)
	}
	_, parts = runPromptTurn(t, withFallback, map[string]any{"type": "stream-start", "modelId": "reported"}, "")
	if got := streamStartModel(parts); got != "reported" {
		t.Fatalf("modelId = %q, want the reported model", got)
	}
	_, parts = runPromptTurn(t, nil, map[string]any{"type": "stream-start"}, "")
	if got := streamStartModel(parts); got != "" {
		t.Fatalf("modelId = %q, want none without a fallback", got)
	}
}

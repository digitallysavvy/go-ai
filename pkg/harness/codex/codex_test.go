package codex_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
)

// TS: "declares the harness id and builtin tools"
func TestBuiltinTools_Declared(t *testing.T) {
	h := codex.New(codex.Settings{})
	if h.HarnessID() != "codex" {
		t.Fatalf("HarnessID = %q", h.HarnessID())
	}
	tools := h.BuiltinTools()
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2 (bash, webSearch)", len(tools))
	}
	if tools["bash"].NativeName != "shell" || tools["bash"].ToolUseKind != harness.BuiltinToolUseKindBash {
		t.Errorf("bash tool = %+v", tools["bash"])
	}
	if tools["webSearch"].NativeName != "web_search" {
		t.Errorf("webSearch.NativeName = %q, want web_search", tools["webSearch"].NativeName)
	}
	if harness.SupportsBuiltinToolApprovals(h) {
		t.Error("codex must not support built-in tool approvals")
	}
	if harness.SupportsBuiltinToolFiltering(h) {
		t.Error("codex must not support built-in tool filtering")
	}
}

// TS: "rejects built-in tool filtering controls"
func TestDoStart_RejectsBuiltinToolFiltering(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h := codex.New(codex.Settings{MintBridgeToken: func(string) string { return "tok" }})
	_, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
		BuiltinToolFiltering: &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow, ToolNames: []string{"bash"}},
	})
	if _, ok := err.(*harness.CapabilityUnsupportedError); !ok {
		t.Fatalf("error = %v, want *harness.CapabilityUnsupportedError", err)
	}
}

// TS: "rejects built-in permission modes other than allow-all"
func TestDoStart_RejectsNonAllowAllPermissionMode(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h := codex.New(codex.Settings{MintBridgeToken: func(string) string { return "tok" }})
	_, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
		PermissionMode: harness.PermissionModeAllowReads,
	})
	if _, ok := err.(*harness.CapabilityUnsupportedError); !ok {
		t.Fatalf("error = %v, want *harness.CapabilityUnsupportedError", err)
	}
}

// TS: "throws HarnessCapabilityUnsupportedError when the network sandbox
// session exposes no ports"
func TestDoStart_NoPortsUnsupportedError(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	sandbox.ports = nil
	wireSpawn(sandbox)
	h := codex.New(codex.Settings{MintBridgeToken: func(string) string { return "tok" }})
	_, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	if _, ok := err.(*harness.CapabilityUnsupportedError); !ok {
		t.Fatalf("error = %v, want *harness.CapabilityUnsupportedError", err)
	}
}

func startedHarness(t *testing.T, settings codex.Settings, onStart func(turn *bridgetest.Turn, start map[string]any)) (*codex.Harness, harness.Session) {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: onStart})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	settings.MintBridgeToken = func(string) string { return "tok" }
	settings.StartupTimeout = 2 * time.Second
	h := codex.New(settings)
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	return h, sess
}

func finishFrame() map[string]any {
	return map[string]any{
		"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
	}
}

// TS: "falls back to the default model, overridden by the per-turn model" +
// "sends configured Codex config to the bridge"
func TestDoPromptTurn_SendsStartFields(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	webSearch := true
	_, sess := startedHarness(t, codex.Settings{
		ReasoningEffort: "high", CodexConfig: map[string]any{"model_reasoning_summary": "detailed"}, WebSearch: &webSearch,
	}, func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
		turn.Emit(finishFrame())
	})

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-captureDone:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn never finished")
	}
	if err := control.Err(); err != nil {
		t.Fatalf("control.Err() = %v", err)
	}

	if captured["model"] != codex.DefaultModel {
		t.Errorf("model = %v, want %s", captured["model"], codex.DefaultModel)
	}
	if captured["reasoningEffort"] != "high" {
		t.Errorf("reasoningEffort = %v, want high", captured["reasoningEffort"])
	}
	if captured["webSearch"] != true {
		t.Errorf("webSearch = %v, want true", captured["webSearch"])
	}
	cfg, _ := captured["codexConfig"].(map[string]any)
	if cfg["model_reasoning_summary"] != "detailed" {
		t.Errorf("codexConfig = %v", cfg)
	}
}

// TS: "falls back to the default model, overridden by the per-turn model"
func TestDoPromptTurn_PerTurnModelOverridesDefault(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	_, sess := startedHarness(t, codex.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
		turn.Emit(finishFrame())
	})
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Model: "gpt-6"},
		Prompt:       harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	<-captureDone
	<-control.Done()
	if captured["model"] != "gpt-6" {
		t.Errorf("model = %v, want gpt-6", captured["model"])
	}
}

// TS: "does not start a bridge turn when the signal is already aborted"
// (mirrored from claude-code; codex has the same abort semantics).
func TestDoPromptTurn_AbortedContextSkipsStart(t *testing.T) {
	started := make(chan struct{}, 1)
	_, sess := startedHarness(t, codex.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		started <- struct{}{}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	control, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("control never settled after an already-aborted context")
	}
	if control.Err() == nil {
		t.Error("control.Err() = nil, want a cancellation error")
	}
	select {
	case <-started:
		t.Fatal("bridge received a start frame despite the aborted context")
	case <-time.After(100 * time.Millisecond):
	}
}

// End-to-end: a tool call is forwarded, the host submits a result, and the
// bridge finishes the turn.
func TestDoPromptTurn_ToolCallRoundTrip(t *testing.T) {
	_, sess := startedHarness(t, codex.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		turn.Emit(map[string]any{"type": "tool-call", "toolCallId": "call-1", "toolName": "bash", "input": `{"command":"ls"}`})
		time.Sleep(30 * time.Millisecond)
		turn.Emit(finishFrame())
	})

	var mu sync.Mutex
	var parts []harness.StreamPart
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"),
		Emit: func(p harness.StreamPart) {
			mu.Lock()
			parts = append(parts, p)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}

	sawToolCall := func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range parts {
			if p.PartType() == harness.PartTypeToolCall {
				return true
			}
		}
		return false
	}
	deadline := time.After(2 * time.Second)
	for !sawToolCall() {
		select {
		case <-deadline:
			t.Fatal("tool-call was never forwarded")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := control.SubmitToolResult(context.Background(), harness.ToolResultSubmission{ToolCallID: "call-1", Output: "ok"}); err != nil {
		t.Fatalf("SubmitToolResult: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn never settled")
	}
}

// TS: "does not support manual compaction"
func TestDoCompact_Unsupported(t *testing.T) {
	_, sess := startedHarness(t, codex.Settings{}, func(*bridgetest.Turn, map[string]any) {})
	err := sess.DoCompact(context.Background(), "")
	if _, ok := err.(*harness.CapabilityUnsupportedError); !ok {
		t.Fatalf("error = %v, want *harness.CapabilityUnsupportedError", err)
	}
}

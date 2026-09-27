package claudecode_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// TS: "declares the harness id and builtin tools"
func TestBuiltinTools_Declared(t *testing.T) {
	h, err := claudecode.New(claudecode.Settings{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if h.HarnessID() != "claude-code" {
		t.Fatalf("HarnessID = %q", h.HarnessID())
	}
	tools := h.BuiltinTools()
	for _, name := range []string{"read", "write", "edit", "bash", "glob", "grep", "webSearch", "askUserQuestions", "WebFetch", "TodoWrite", "Skill", "ToolSearch", "Monitor"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("builtin tool %q missing", name)
		}
	}
	if tools["read"].NativeName != "Read" || tools["read"].ToolUseKind != harness.BuiltinToolUseKindReadonly {
		t.Errorf("read tool = %+v", tools["read"])
	}
	if tools["bash"].NativeName != "Bash" || tools["bash"].ToolUseKind != harness.BuiltinToolUseKindBash {
		t.Errorf("bash tool = %+v", tools["bash"])
	}
	if tools["askUserQuestions"].NativeName != "AskUserQuestion" {
		t.Errorf("askUserQuestions.NativeName = %q, want AskUserQuestion", tools["askUserQuestions"].NativeName)
	}
	if !h.SupportsBuiltinToolApprovals() || !h.SupportsBuiltinToolFiltering() {
		t.Error("claude-code must support builtin tool approvals and filtering")
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

	h, err := claudecode.New(claudecode.Settings{MintBridgeToken: func(string) string { return "tok" }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	var capErr *harness.CapabilityUnsupportedError
	if err == nil {
		t.Fatal("expected an error")
	}
	if !asCapabilityUnsupported(err, &capErr) {
		t.Fatalf("error = %v, want *harness.CapabilityUnsupportedError", err)
	}
}

func asCapabilityUnsupported(err error, target **harness.CapabilityUnsupportedError) bool {
	for err != nil {
		if ce, ok := err.(*harness.CapabilityUnsupportedError); ok {
			*target = ce
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// startedSandbox spins up a fake bridge server + sandbox pair wired together
// with a fixed token, ready for h.DoStart.
func startedHarness(t *testing.T, settings claudecode.Settings, onStart func(turn *bridgetest.Turn, start map[string]any)) (*claudecode.Harness, harness.Session) {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: onStart})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	settings.MintBridgeToken = func(string) string { return "tok" }
	settings.StartupTimeout = 2 * time.Second
	h, err := claudecode.New(settings)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	return h, sess
}

// TS: "sends the per-turn model to the CLI" + "sends the thinking
// configuration and effort to the bridge" + "sends environment configuration
// to the bridge"
func TestDoPromptTurn_SendsStartFields(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	_, sess := startedHarness(t, claudecode.Settings{
		Effort: "high", Env: map[string]string{"FOO": "bar"},
	}, func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})

	var parts []harness.StreamPart
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Model: "claude-x"},
		Prompt:       harness.TextPrompt("hi"),
		Emit:         func(p harness.StreamPart) { parts = append(parts, p) },
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

	if captured["model"] != "claude-x" {
		t.Errorf("model = %v, want claude-x", captured["model"])
	}
	if captured["effort"] != "high" {
		t.Errorf("effort = %v, want high", captured["effort"])
	}
	thinking, _ := captured["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
		t.Errorf("thinking = %v, want adaptive/summarized default", thinking)
	}
	env, _ := captured["env"].(map[string]any)
	if env["FOO"] != "bar" {
		t.Errorf("env = %v, want FOO=bar", env)
	}
	foundFinish := false
	for _, p := range parts {
		if p.PartType() == harness.PartTypeFinish {
			foundFinish = true
		}
	}
	if !foundFinish {
		t.Error("finish part was never forwarded to Emit")
	}
}

// TS: "does not start a bridge turn when the signal is already aborted"
func TestDoPromptTurn_AbortedContextSkipsStart(t *testing.T) {
	started := make(chan struct{}, 1)
	_, sess := startedHarness(t, claudecode.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
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
	_, sess := startedHarness(t, claudecode.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		turn.Emit(map[string]any{"type": "tool-call", "toolCallId": "call-1", "toolName": "read", "input": `{"file_path":"a.txt"}`})
		time.Sleep(30 * time.Millisecond)
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
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
	if err := control.SubmitToolResult(context.Background(), harness.ToolResultSubmission{ToolCallID: "call-1", Output: "file contents"}); err != nil {
		t.Fatalf("SubmitToolResult: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn never settled")
	}
}

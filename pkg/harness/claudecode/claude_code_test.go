package claudecode_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

// TS `claude-code-harness.ts` CLAUDE_CODE_BUILTIN_TOOLS: the complete
// 47-entry table (7 common tools + askUserQuestions + 39 native-only tools).
func TestBuiltinTools_CompleteTable(t *testing.T) {
	h, err := claudecode.New(claudecode.Settings{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := h.BuiltinTools()
	want := []string{
		"read", "write", "edit", "bash", "glob", "grep", "webSearch",
		"WebFetch", "NotebookEdit", "TodoWrite", "Agent", "TaskCreate", "TaskGet",
		"TaskUpdate", "TaskList", "TaskStop", "TaskOutput", "Monitor",
		"ListMcpResources", "ListMcpResourcesTool", "ReadMcpResource", "ReadMcpResourceTool",
		"ReadMcpResourceDirTool", "RefreshMcpTools", "ExitPlanMode", "EnterPlanMode",
		"EnterWorktree", "ExitWorktree", "askUserQuestions", "Skill", "ToolSearch",
		"Artifact", "CronCreate", "CronDelete", "CronList", "DesignSync", "LSP",
		"PowerShell", "PushNotification", "RemoteTrigger", "ReportFindings",
		"ScheduleWakeup", "SendMessage", "SendUserFile", "ShareOnboardingGuide",
		"WaitForMcpServers", "Workflow",
	}
	if len(want) != 47 {
		t.Fatalf("test bug: want list has %d entries, expected 47", len(want))
	}
	if len(tools) != len(want) {
		t.Errorf("len(BuiltinTools()) = %d, want %d", len(tools), len(want))
	}
	for _, name := range want {
		if _, ok := tools[name]; !ok {
			t.Errorf("builtin tool %q missing", name)
		}
	}
	for name := range tools {
		found := false
		for _, w := range want {
			if w == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected builtin tool %q not in the TS table", name)
		}
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

// TS: "sends configured subagent activity options to the bridge"
// (claude-code-harness.test.ts)
func TestDoPromptTurn_SendsSubagentActivityOptions(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	_, sess := startedHarness(t, claudecode.Settings{
		AgentProgressSummaries: true, ForwardSubagentText: true,
	}, func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"),
		Emit:   func(harness.StreamPart) {},
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

	if captured["agentProgressSummaries"] != true {
		t.Errorf("agentProgressSummaries = %v, want true", captured["agentProgressSummaries"])
	}
	if captured["forwardSubagentText"] != true {
		t.Errorf("forwardSubagentText = %v, want true", captured["forwardSubagentText"])
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

// TS claude-code-harness.test.ts "exposes steering when the bridge
// advertises acknowledged user messages" (submitUserMessage / mid-turn
// steering). bridgetest.Server always advertises
// experimental_userMessageResponses on hello, so PromptControl must satisfy
// harness.UserMessageSubmitter and a submitted message must round-trip
// through the bridge's accept/reject protocol.
func TestDoPromptTurn_SubmitUserMessage(t *testing.T) {
	_, sess := startedHarness(t, claudecode.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {})

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}

	submitter, ok := control.(harness.UserMessageSubmitter)
	if !ok {
		t.Fatal("control does not implement harness.UserMessageSubmitter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := submitter.SubmitUserMessage(ctx, "steer this"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
}

// TS claude-code-harness.test.ts "submits compaction without requiring an
// active acknowledged turn": doCompact rides the same `user-message` frame
// type but fire-and-forget (no messageId/ack), even with no turn active.
func TestDoCompact_SendsUserMessageFireAndForget(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnUserMessage: func(messageID, text string) (bool, string) {
		mu.Lock()
		received = append(received, map[string]any{"messageId": messageID, "text": text})
		mu.Unlock()
		return true, ""
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)
	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout:  2 * time.Second,
		MintBridgeToken: func(string) string { return "tok" },
	})
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

	if err := sess.DoCompact(context.Background(), "keep the error trace"); err != nil {
		t.Fatalf("DoCompact: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("bridge never received the compact user-message")
		case <-time.After(5 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if received[0]["messageId"] != "" {
		t.Errorf("compact user-message has messageId = %v, want empty (fire-and-forget)", received[0]["messageId"])
	}
	if received[0]["text"] != "/compact keep the error trace" {
		t.Errorf("compact text = %v, want '/compact keep the error trace'", received[0]["text"])
	}
}

// TS: "uses a caller-minted bridge token and reuses it when attaching" +
// "resumes the exact conversation after detaching and attaching" (rung 1 —
// ATTACH, parked session: DoDetach persists live bridge coordinates; a later
// DoStart with ResumeFrom reopens a socket to the same still-running bridge
// instead of respawning it).
func TestDoStart_AttachesToParkedBridge_NoRespawn(t *testing.T) {
	var mintCalls int32
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout: 2 * time.Second,
		MintBridgeToken: func(string) string {
			atomic.AddInt32(&mintCalls, 1)
			return "tok"
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	if sandbox.SpawnCount() != 1 {
		t.Fatalf("SpawnCount after initial DoStart = %d, want 1", sandbox.SpawnCount())
	}

	resumeState, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	if resumeState.HarnessID != claudecode.HarnessID {
		t.Fatalf("resumeState.HarnessID = %q", resumeState.HarnessID)
	}

	attached, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: resumeState,
	})
	if err != nil {
		t.Fatalf("DoStart (attach): %v", err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })

	if sandbox.SpawnCount() != 1 {
		t.Fatalf("SpawnCount after attach = %d, want 1 (attach must not respawn)", sandbox.SpawnCount())
	}
	if atomic.LoadInt32(&mintCalls) != 1 {
		t.Fatalf("mintBridgeToken called %d times, want 1 (attach reuses the persisted token)", mintCalls)
	}
	if !attached.IsResume() {
		t.Error("attached session.IsResume() = false, want true")
	}

	// The attached session must still be able to run a turn over the reused
	// connection (the fake server here answers with no OnStart handler, so
	// the assertion below is only that the frame reached the live bridge
	// without erroring the send).
	control, err := attached.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn after attach: %v", err)
	}
	select {
	case <-control.Done():
		t.Fatalf("control settled immediately: %v", control.Err())
	case <-time.After(100 * time.Millisecond):
		// Expected: no OnStart handler is configured on srv, so the turn
		// just sits open — proves the frame reached the live bridge over the
		// reused connection without error.
	}
}

// TS: attach rung 2 — a still-running bridge that has gone unreachable (bad
// persisted token) falls through to a normal respawn using the resume/rerun
// fallback (`continue`/`resumeSessionId`), exactly like a session with no
// bridge coordinates at all.
func TestDoStart_AttachFailureFallsBackToRespawn(t *testing.T) {
	var starts int32
	var lastStart map[string]any
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		atomic.AddInt32(&starts, 1)
		lastStart = start
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout:  2 * time.Second,
		MintBridgeToken: func(string) string { return "tok" },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Build a ResumeSessionState by hand carrying bridge coordinates with a
	// wrong token, as if the persisted bridge is no longer reachable.
	resumeState, err := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{
		"bridge":          map[string]any{"port": 4319, "token": "stale-token", "lastSeenEventId": 0},
		"claudeSessionId": "claude-session-1",
	})
	if err != nil {
		t.Fatalf("NewResumeSessionState: %v", err)
	}

	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ResumeFrom: resumeState,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	if sandbox.SpawnCount() != 1 {
		t.Fatalf("SpawnCount = %d, want 1 (attach failed, so DoStart must fall back to a single respawn)", sandbox.SpawnCount())
	}

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn never finished")
	}
	if atomic.LoadInt32(&starts) != 1 {
		t.Fatalf("bridge received %d start frames, want 1", starts)
	}
	if lastStart["resumeSessionId"] != "claude-session-1" {
		t.Errorf("start.resumeSessionId = %v, want claude-session-1 (rerun fallback resumes the Claude conversation)", lastStart["resumeSessionId"])
	}
}

// TS commit 64a0ff2 ("avoid sending custom user-agent for non-AI Gateway
// requests") + claude-code-harness.test.ts "sets the client app for AI
// Gateway auth".
func TestDoStart_ClientAppSetForAIGatewayAuth(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout:  2 * time.Second,
		MintBridgeToken: func(string) string { return "tok" },
		Auth:            harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "gateway-key"}),
	})
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

	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-captureDone:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}

	env, _ := captured["env"].(map[string]any)
	if env["CLAUDE_AGENT_SDK_CLIENT_APP"] != "ai-sdk/harness-claude-code/1.0.142" {
		t.Errorf("env.CLAUDE_AGENT_SDK_CLIENT_APP = %v, want ai-sdk/harness-claude-code/1.0.142", env["CLAUDE_AGENT_SDK_CLIENT_APP"])
	}
}

// TS commit 64a0ff2 + claude-code-harness.test.ts "does not set the client
// app for direct Anthropic auth".
func TestDoStart_ClientAppNotSetForDirectAuth(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout:  2 * time.Second,
		MintBridgeToken: func(string) string { return "tok" },
		Auth:            harness.AuthEnvironment(map[string]string{"ANTHROPIC_API_KEY": "anthropic-key"}),
	})
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

	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-captureDone:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}

	env, _ := captured["env"].(map[string]any)
	if _, ok := env["CLAUDE_AGENT_SDK_CLIENT_APP"]; ok {
		t.Errorf("env has CLAUDE_AGENT_SDK_CLIENT_APP = %v, want it absent for direct auth", env["CLAUDE_AGENT_SDK_CLIENT_APP"])
	}
	if env["ANTHROPIC_API_KEY"] != "anthropic-key" {
		t.Errorf("env.ANTHROPIC_API_KEY = %v, want anthropic-key", env["ANTHROPIC_API_KEY"])
	}
}

// TS commit cdc12a1 ("add support for using adapter-native subscriptions
// where available"): with no explicit `auth` and no direct env credential,
// DoStart must fall back to the native `claude login` subscription
// credential (~/.claude/.credentials.json) instead of leaving the bridge
// with no Anthropic credential at all.
func TestDoStart_UsesNativeSubscriptionWhenNoExplicitCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")

	claudeDir := home + "/.claude"
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cred := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken": "subscription-token", "refreshToken": "r", "expiresAt": float64(time.Now().Add(24 * time.Hour).UnixMilli()),
		},
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(claudeDir+"/.credentials.json", raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var captured map[string]any
	captureDone := make(chan struct{})
	_, sess := startedHarness(t, claudecode.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
	})
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-captureDone:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}

	env, _ := captured["env"].(map[string]any)
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "subscription-token" {
		t.Errorf("env.CLAUDE_CODE_OAUTH_TOKEN = %v, want subscription-token", env["CLAUDE_CODE_OAUTH_TOKEN"])
	}
}

// TS rung 2 — REPLAY: when a continued (suspended) turn's persisted bridge is
// unreachable (attach fails) and its on-disk event-log.ndjson last line is a
// finished turn, the respawned bridge is asked to replay it from disk
// (`BRIDGE_REPLAY_FROM_DISK=1`) and the channel opens with `resume: true`.
func TestDoContinueTurn_ReplaysDiskLogOnRespawn(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)

	var mu sync.Mutex
	var spawnEnv map[string]string
	sandbox.SetSpawn(func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		mu.Lock()
		spawnEnv = opts.Env
		mu.Unlock()
		atomic.AddInt32(&sandbox.spawnCount, 1)
		proc := bridgetest.NewProcess()
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
		go func() {
			time.Sleep(50 * time.Millisecond)
			proc.Exit(0)
		}()
		return proc, nil
	})

	// Seed the on-disk event log the respawned bridge would read, ending in
	// a finished turn.
	const bridgeStateDir = "/home/agent/.ai-sdk-harness/.agent-runs/s1/bridge"
	if err := sandbox.WriteTextFile(context.Background(), providerutils.SandboxWriteTextFileOptions{
		Path: bridgeStateDir + "/event-log.ndjson",
		Content: `{"seq":1,"type":"text-start"}` + "\n" +
			`{"seq":2,"type":"finish","finishReason":{"unified":"stop","raw":"stop"}}`,
	}); err != nil {
		t.Fatalf("WriteTextFile: %v", err)
	}

	h, err := claudecode.New(claudecode.Settings{
		StartupTimeout:  2 * time.Second,
		MintBridgeToken: func(string) string { return "tok" },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A stale token makes the attach attempt fail, forcing the fall-through
	// to rung 2/3.
	continueState, err := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{
		"bridge":          map[string]any{"port": 4319, "token": "stale-token", "lastSeenEventId": 2},
		"claudeSessionId": "claude-session-1",
	})
	if err != nil {
		t.Fatalf("NewContinueTurnState: %v", err)
	}

	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir", ContinueFrom: continueState,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	if sandbox.SpawnCount() != 1 {
		t.Fatalf("SpawnCount = %d, want 1", sandbox.SpawnCount())
	}
	mu.Lock()
	env := spawnEnv
	mu.Unlock()
	if env["BRIDGE_REPLAY_FROM_DISK"] != "1" {
		t.Errorf("spawn env BRIDGE_REPLAY_FROM_DISK = %q, want \"1\" (finished-turn disk log must trigger replay)", env["BRIDGE_REPLAY_FROM_DISK"])
	}
}

// TS `claude-code-harness.ts` onDiagnostic: `sandbox-log`/`debug-event`
// bridge frames are normalized into harness.Diagnostic and forwarded to
// StartOptions.Observability.Report, stamped with the session id. Diagnostic
// frames must not be delivered to the ordinary stream-part listener.
func TestDoStart_ReportsDiagnostics(t *testing.T) {
	var mu sync.Mutex
	var diags []harness.Diagnostic
	reported := make(chan struct{}, 2)
	report := func(d harness.Diagnostic) {
		mu.Lock()
		diags = append(diags, d)
		mu.Unlock()
		reported <- struct{}{}
	}

	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		turn.Emit(map[string]any{"type": "sandbox-log", "source": "claude", "stream": "stderr", "line": "boom"})
		turn.Emit(map[string]any{"type": "debug-event", "level": "warn", "subsystem": "claude-code.bridge", "message": "retrying"})
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h, err := claudecode.New(claudecode.Settings{MintBridgeToken: func(string) string { return "tok" }, StartupTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "diag-session", SandboxSession: sandbox, SessionWorkDir: "/workdir",
		Observability: &harness.Observability{Report: report},
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn never finished")
	}
	for i := 0; i < 2; i++ {
		select {
		case <-reported:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/2 diagnostics reported", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(diags) != 2 {
		t.Fatalf("len(diags) = %d, want 2", len(diags))
	}
	logDiag, eventDiag := diags[0], diags[1]
	if logDiag.Kind != "log" || logDiag.Message != "boom" || logDiag.Level != harness.DebugLevelWarn || logDiag.SessionID != "diag-session" {
		t.Errorf("log diagnostic = %+v", logDiag)
	}
	if eventDiag.Kind != "event" || eventDiag.Message != "retrying" || eventDiag.Subsystem != "claude-code.bridge" || eventDiag.SessionID != "diag-session" {
		t.Errorf("event diagnostic = %+v", eventDiag)
	}
}

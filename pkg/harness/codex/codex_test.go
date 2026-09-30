package codex_test

import (
	"context"
	"sync"
	"sync/atomic"
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
	if len(tools) != 4 {
		t.Fatalf("len(tools) = %d, want 4 (bash, webSearch, apply_patch, view_image)", len(tools))
	}
	if tools["bash"].NativeName != "shell" || tools["bash"].ToolUseKind != harness.BuiltinToolUseKindBash {
		t.Errorf("bash tool = %+v", tools["bash"])
	}
	if tools["webSearch"].NativeName != "web_search" {
		t.Errorf("webSearch.NativeName = %q, want web_search", tools["webSearch"].NativeName)
	}
	if tools["apply_patch"].ToolUseKind != harness.BuiltinToolUseKindEdit {
		t.Errorf("apply_patch.ToolUseKind = %q, want edit", tools["apply_patch"].ToolUseKind)
	}
	if tools["view_image"].ToolUseKind != harness.BuiltinToolUseKindReadonly {
		t.Errorf("view_image.ToolUseKind = %q, want readonly", tools["view_image"].ToolUseKind)
	}
	if harness.SupportsBuiltinToolApprovals(h) {
		t.Error("codex must not support built-in tool approvals")
	}
	// TS d17ead78cd: `supportsBuiltinToolFiltering: true` (app-server filters
	// bash/webSearch/view_image via config and apply_patch via a trusted
	// hook, both bridge-internal).
	if !harness.SupportsBuiltinToolFiltering(h) {
		t.Error("codex must support built-in tool filtering")
	}
}

// TS "forwards built-in tool filtering to the bridge".
func TestDoPromptTurn_ForwardsBuiltinToolFiltering(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	filtering := &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringDeny, ToolNames: []string{"bash"}}
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
		turn.Emit(finishFrame())
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h := codex.New(codex.Settings{MintBridgeToken: func(string) string { return "tok" }, StartupTimeout: 2 * time.Second})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
		BuiltinToolFiltering: filtering,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	_, err = sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hello"), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-captureDone:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}

	gotFiltering, _ := captured["builtinToolFiltering"].(map[string]any)
	if gotFiltering["mode"] != "deny" {
		t.Errorf("start.builtinToolFiltering.mode = %v, want deny", gotFiltering["mode"])
	}
	names, _ := gotFiltering["toolNames"].([]any)
	if len(names) != 1 || names[0] != "bash" {
		t.Errorf("start.builtinToolFiltering.toolNames = %v, want [bash]", gotFiltering["toolNames"])
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

// TS codex-instructions.test.ts "attaches a parked session without
// replaying old turn events" (rung 1 — ATTACH: a DoDetach's persisted bridge
// coordinates let a later DoStart reopen a socket to the same still-running
// bridge instead of respawning it).
func TestDoStart_AttachesToParkedBridge_NoRespawn(t *testing.T) {
	var mintCalls int32
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h := codex.New(codex.Settings{
		StartupTimeout: 2 * time.Second,
		MintBridgeToken: func(string) string {
			atomic.AddInt32(&mintCalls, 1)
			return "tok"
		},
	})
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
}

// TS: attach rung 2 — an unreachable persisted bridge (the process is gone;
// its port no longer accepts connections) falls through to a normal respawn
// using the resumeThreadId rerun fallback.
func TestDoStart_AttachFailureFallsBackToRespawn(t *testing.T) {
	var starts int32
	var lastStart map[string]any
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		atomic.AddInt32(&starts, 1)
		lastStart = start
		turn.Emit(finishFrame())
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h := codex.New(codex.Settings{
		StartupTimeout:  2 * time.Second,
		MintBridgeToken: func(string) string { return "tok" },
	})

	// Port 9999 does not match sandbox.Ports()[0] (4319), so
	// testSandbox.GetPortEndpoint resolves it to a closed local port: the
	// attach connect is refused immediately, exactly like a bridge process
	// that no longer exists.
	resumeState, err := harness.NewResumeSessionState(codex.HarnessID, map[string]any{
		"bridge":   map[string]any{"port": 9999, "token": "tok", "lastSeenEventId": 0},
		"threadId": "thread-1",
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
	if lastStart["resumeThreadId"] != "thread-1" {
		t.Errorf("start.resumeThreadId = %v, want thread-1 (rerun fallback resumes the Codex thread)", lastStart["resumeThreadId"])
	}
}

// TS codex-instructions.test.ts "passes dynamic tools without changing user
// messages": since the d17ead78cd app-server migration, tools are declared
// only in the ordinary `tools` start-frame field (the bridge registers them
// as app-server "dynamic tools" and handles tool-call/tool-result frames);
// there is no more CLI-relay prompt-text framing of the user message, and no
// separate MCP-server registration path on the host side.
func TestDoPromptTurn_ToolsForwardedPlainlyWithoutPromptFraming(t *testing.T) {
	var captured map[string]any
	captureDone := make(chan struct{})
	_, sess := startedHarness(t, codex.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		captured = start
		close(captureDone)
		turn.Emit(finishFrame())
	})

	tool := harness.ToolSpec{Name: "weather", Description: "Get the weather", InputSchema: map[string]any{"type": "object"}}
	_, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("what's the weather"), Emit: func(harness.StreamPart) {},
		TurnSettings: harness.TurnSettings{Tools: []harness.ToolSpec{tool}},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-captureDone:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
	}

	if prompt, _ := captured["prompt"].(string); prompt != "what's the weather" {
		t.Errorf("prompt = %q, want the raw user text unchanged", prompt)
	}
	tools, _ := captured["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("start.tools = %v, want exactly the one declared host tool", captured["tools"])
	}
	toolEntry, _ := tools[0].(map[string]any)
	if toolEntry["name"] != "weather" {
		t.Errorf("start.tools[0].name = %v, want weather", toolEntry["name"])
	}
	if _, hasMCPServers := captured["mcpServers"]; hasMCPServers {
		t.Errorf("start frame declares mcpServers = %v; host tools must not be registered as MCP tools", captured["mcpServers"])
	}
}

// TS "waits for Codex to accept steering and rejects messages after the turn
// finishes": mid-turn steering (`submitUserMessage`/`turn/steer`) is wired
// unconditionally by wireTurn (not gated on a bridge-advertised hello
// capability, unlike claude-code/opencode).
func TestDoPromptTurn_SubmitUserMessage(t *testing.T) {
	_, sess := startedHarness(t, codex.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {})

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
	if err := submitter.SubmitUserMessage(ctx, "Actually, Paris, Texas."); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
}

// TS `codex-harness.ts` onDiagnostic: `sandbox-log`/`debug-event` bridge
// frames are normalized into harness.Diagnostic and forwarded to
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
		turn.Emit(map[string]any{"type": "sandbox-log", "source": "codex", "stream": "stderr", "line": "boom"})
		turn.Emit(map[string]any{"type": "debug-event", "level": "warn", "subsystem": "codex.bridge", "message": "retrying"})
		turn.Emit(finishFrame())
	}})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	wireSpawn(sandbox)

	h := codex.New(codex.Settings{MintBridgeToken: func(string) string { return "tok" }, StartupTimeout: 2 * time.Second})
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
	if eventDiag.Kind != "event" || eventDiag.Message != "retrying" || eventDiag.Subsystem != "codex.bridge" || eventDiag.SessionID != "diag-session" {
		t.Errorf("event diagnostic = %+v", eventDiag)
	}
}

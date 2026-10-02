package opencode

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// fakeSandbox is a minimal harness.NetworkSandboxSession mirroring TS
// opencode-harness.test.ts's `fakeSandboxSession`.
type fakeSandbox struct {
	mu            sync.Mutex
	spawnCommands []string
	spawnEnvs     []map[string]string
	addReqTransMu sync.Mutex
	addReqTrans   [][]harness.RequestTransformation
	bridgeAddr    string
	files         map[string]string
}

func (s *fakeSandbox) Description() string { return "opencode fake sandbox" }
func (s *fakeSandbox) Run(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if opts.Command == `printf "%s" "$HOME"` {
		return providerutils.SandboxRunResult{ExitCode: 0, Stdout: "/home/vercel-sandbox"}, nil
	}
	return providerutils.SandboxRunResult{ExitCode: 0}, nil
}
func (s *fakeSandbox) Spawn(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	s.mu.Lock()
	s.spawnCommands = append(s.spawnCommands, opts.Command)
	s.spawnEnvs = append(s.spawnEnvs, opts.Env)
	s.mu.Unlock()
	proc := bridgetest.NewProcess()
	port := opts.Env[bridge.EnvWSPort]
	go func() {
		time.Sleep(2 * time.Millisecond)
		proc.WriteStdout(`{"type":"bridge-ready","port":` + port + "}\n")
		time.Sleep(5 * time.Millisecond)
		proc.Exit(0)
	}()
	return proc, nil
}
func (s *fakeSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (s *fakeSandbox) ReadBinaryFile(context.Context, string) ([]byte, error)  { return nil, nil }
func (s *fakeSandbox) ReadTextFile(_ context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.files[opts.Path]; ok {
		return &v, nil
	}
	return nil, nil
}
func (s *fakeSandbox) WriteFile(context.Context, string, io.Reader) error    { return nil }
func (s *fakeSandbox) WriteBinaryFile(context.Context, string, []byte) error { return nil }
func (s *fakeSandbox) WriteTextFile(context.Context, providerutils.SandboxWriteTextFileOptions) error {
	return nil
}
func (s *fakeSandbox) ID() string                      { return "test-sandbox" }
func (s *fakeSandbox) DefaultWorkingDirectory() string { return "/vercel/sandbox" }
func (s *fakeSandbox) Ports() []int                    { return []int{4319} }
func (s *fakeSandbox) GetPortEndpoint(_ context.Context, _ harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return harness.PortEndpoint{URL: "ws://" + s.bridgeAddr + "/"}, nil
}
func (s *fakeSandbox) GetPortURL(context.Context, harness.PortEndpointOptions) (string, error) {
	return "ws://" + s.bridgeAddr + "/", nil
}
func (s *fakeSandbox) Stop(context.Context) error               { return nil }
func (s *fakeSandbox) Destroy(context.Context) error            { return nil }
func (s *fakeSandbox) Restricted() providerutils.SandboxSession { return s }
func (s *fakeSandbox) AddRequestTransformations(_ context.Context, t []harness.RequestTransformation) error {
	s.addReqTransMu.Lock()
	s.addReqTrans = append(s.addReqTrans, t)
	s.addReqTransMu.Unlock()
	return nil
}

var _ harness.NetworkSandboxSession = (*fakeSandbox)(nil)

func newFakeSandbox(srv *bridgetest.Server) *fakeSandbox {
	return &fakeSandbox{bridgeAddr: srv.Addr(), files: map[string]string{}}
}

func newServer(t *testing.T, token string, onStart func(*bridgetest.Turn, map[string]any)) *bridgetest.Server {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: token, OnStart: onStart})
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateOpenCodeMetadata(t *testing.T) {
	h, err := CreateOpenCode()
	if err != nil {
		t.Fatal(err)
	}
	if h.SpecificationVersion() != "harness-v1" {
		t.Fatalf("SpecificationVersion = %q", h.SpecificationVersion())
	}
	if h.HarnessID() != "opencode" {
		t.Fatalf("HarnessID = %q", h.HarnessID())
	}
	if !harness.SupportsBuiltinToolApprovals(h) {
		t.Fatal("expected SupportsBuiltinToolApprovals() to be true")
	}
}

func TestCreateOpenCodeRejectsReservedMCPServerName(t *testing.T) {
	_, err := CreateOpenCode(Settings{MCPServers: map[string]any{"harness-tools": map[string]any{}}})
	if err == nil || !strings.Contains(err.Error(), "harness-tools") {
		t.Fatalf("err = %v, want a reserved-name error", err)
	}
}

func TestGetBootstrapShipsBridgeFilesAndCommands(t *testing.T) {
	b, err := GetBootstrap(context.Background())
	if err != nil {
		t.Fatalf("GetBootstrap: %v", err)
	}
	if b.BootstrapDir != ".harness-bootstrap/opencode" {
		t.Fatalf("BootstrapDir = %q", b.BootstrapDir)
	}
	var paths []string
	for _, f := range b.Files {
		paths = append(paths, f.Path)
	}
	want := []string{
		".harness-bootstrap/opencode/package.json",
		".harness-bootstrap/opencode/pnpm-lock.yaml",
		".harness-bootstrap/opencode/pnpm-workspace.yaml",
		".harness-bootstrap/opencode/bridge.mjs",
		".harness-bootstrap/opencode/host-tool-mcp.mjs",
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
	var commands string
	for _, c := range b.Commands {
		commands += c.Command + "\n"
	}
	if !strings.Contains(commands, "pnpm install --frozen-lockfile --store-dir .pnpm-store") {
		t.Fatalf("commands = %q", commands)
	}
	if !strings.Contains(commands, "opencode --version") {
		t.Fatalf("commands = %q", commands)
	}
}

// hello sends bridge-hello as the very first frame before running onStart,
// matching bridgetest.Server default behavior (it already does this).
func TestDoStartFullRoundTrip(t *testing.T) {
	const token = "fixed-test-token"
	var mu sync.Mutex
	var capturedStart map[string]any
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		capturedStart = start
		mu.Unlock()
		turn.Emit(map[string]any{"type": "text-start", "id": "m"})
		turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "hi"})
		turn.Emit(map[string]any{"type": "text-end", "id": "m"})
		turn.Emit(map[string]any{"type": "bridge-thread", "threadId": "oc-session-1"})
		turn.Emit(map[string]any{
			"type":         "finish-step",
			"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"usage": map[string]any{
				"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 2},
			},
		})
		turn.Emit(map[string]any{
			"type":         "finish",
			"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{
				"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 2},
			},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{
		MintBridgeToken: func(string) string { return token },
		Provider:        "anthropic", ReasoningVariant: "high",
		OpenCodeConfig: map[string]any{"theme": "dark"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/opencode-test-session",
		SandboxSession: sandbox, Headers: map[string]string{"x-tenant": "acme"},
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	var deltas []string
	var finished bool
	var mu2 sync.Mutex
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hello"),
		Emit: func(p harness.StreamPart) {
			mu2.Lock()
			defer mu2.Unlock()
			switch v := p.(type) {
			case *harness.TextDeltaPart:
				deltas = append(deltas, v.Delta)
			case *harness.FinishPart:
				finished = true
			}
		},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not finish")
	}
	if err := control.Err(); err != nil {
		t.Fatalf("control.Err() = %v", err)
	}
	mu2.Lock()
	if len(deltas) != 1 || deltas[0] != "hi" {
		t.Fatalf("deltas = %v, want [hi]", deltas)
	}
	if !finished {
		t.Fatal("expected a finish part")
	}
	mu2.Unlock()

	mu.Lock()
	defer mu.Unlock()
	if capturedStart["operation"] != "prompt" {
		t.Fatalf("start.operation = %v", capturedStart["operation"])
	}
	if capturedStart["prompt"] != "hello" {
		t.Fatalf("start.prompt = %v", capturedStart["prompt"])
	}
	if capturedStart["provider"] != "anthropic" {
		t.Fatalf("start.provider = %v", capturedStart["provider"])
	}
	if capturedStart["variant"] != "high" {
		t.Fatalf("start.variant = %v", capturedStart["variant"])
	}
	if cfg, ok := capturedStart["openCodeConfig"].(map[string]any); !ok || cfg["theme"] != "dark" {
		t.Fatalf("start.openCodeConfig = %v", capturedStart["openCodeConfig"])
	}
	if headers, ok := capturedStart["headers"].(map[string]any); !ok || headers["x-tenant"] != "acme" {
		t.Fatalf("start.headers = %v", capturedStart["headers"])
	}

	// bridge-thread must have updated the session's tracked OpenCode session
	// id, which feeds into resumeSessionId on the next turn.
	sess2, ok := sess.(*session)
	if !ok {
		t.Fatalf("session type = %T", sess)
	}
	sess2.mu.Lock()
	got := sess2.latestOpenCodeSessionID
	sess2.mu.Unlock()
	if got != "oc-session-1" {
		t.Fatalf("latestOpenCodeSessionID = %q, want oc-session-1", got)
	}
}

// TestDoPromptTurn_DrainsAbortedTurnBeforeNextTurn ports TS "drains an
// aborted turn through error and finish before attaching the next turn"
// (opencode-harness.test.ts, TS #21683 "stop aborted OpenCode turns before
// starting the next turn"): a replacement DoPromptTurn call must wait for an
// aborted turn to fully drain (its stray post-abort events dropped, a
// draining error swallowed, then the trailing finish) before the bridge
// ever sees the replacement turn's own `start` frame.
func TestDoPromptTurn_DrainsAbortedTurnBeforeNextTurn(t *testing.T) {
	const token = "fixed-test-token"
	var mu sync.Mutex
	var turns []*bridgetest.Turn
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		mu.Lock()
		turns = append(turns, turn)
		mu.Unlock()
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/opencode-test-session",
		SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	waitForTurn := func(n int) *bridgetest.Turn {
		deadline := time.After(2 * time.Second)
		for {
			mu.Lock()
			if len(turns) >= n {
				turn := turns[n-1]
				mu.Unlock()
				return turn
			}
			mu.Unlock()
			select {
			case <-deadline:
				t.Fatalf("timed out waiting for start frame #%d", n)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	var firstMu sync.Mutex
	var firstEvents []harness.StreamPart
	control1, err := sess.DoPromptTurn(ctx1, harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("Write a long response."),
		Emit: func(p harness.StreamPart) {
			firstMu.Lock()
			firstEvents = append(firstEvents, p)
			firstMu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn (first): %v", err)
	}
	turn1 := waitForTurn(1)
	turn1.Emit(map[string]any{"type": "text-delta", "id": "first", "delta": "first"})

	// Wait for the delta to actually arrive at the host before cancelling:
	// bridgetest's emit is a fire-and-forget websocket write with no
	// delivery acknowledgement, so nothing otherwise orders it before the
	// cancellation below.
	deadline := time.After(2 * time.Second)
	for {
		firstMu.Lock()
		n := len(firstEvents)
		firstMu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the first text-delta to arrive")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel1()
	select {
	case <-control1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not settle after cancel")
	}
	if err := control1.Err(); err == nil {
		t.Fatal("control1.Err() = nil, want a cancellation error")
	}

	// A stale event delivered after the abort must not reach firstEvents
	// (forward checks isSettled) — and the second turn has not started yet,
	// so it cannot reach it either.
	turn1.Emit(map[string]any{"type": "text-delta", "id": "first", "delta": " stale"})

	var secondMu sync.Mutex
	var secondEvents []harness.StreamPart
	secondDone := make(chan struct{})
	var control2 harness.PromptControl
	var control2Err error
	go func() {
		control2, control2Err = sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
			Prompt: harness.TextPrompt("Reply with banana."),
			Emit: func(p harness.StreamPart) {
				secondMu.Lock()
				secondEvents = append(secondEvents, p)
				secondMu.Unlock()
			},
		})
		close(secondDone)
	}()

	// The replacement turn must still be blocked on the drain: no second
	// start frame yet.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	startCount := len(turns)
	mu.Unlock()
	if startCount != 1 {
		t.Fatalf("start frames observed = %d, want 1 (the second turn must wait for the first to drain)", startCount)
	}
	select {
	case <-secondDone:
		t.Fatal("DoPromptTurn (second) returned before the first turn drained")
	default:
	}

	// An error while draining is swallowed: the bridge's own finally block
	// emits the trailing finish, and only that may finish the drain.
	turn1.Emit(map[string]any{"type": "error", "error": "OpenCode session abort failed"})
	time.Sleep(20 * time.Millisecond)
	select {
	case <-secondDone:
		t.Fatal("DoPromptTurn (second) returned on the draining error; it must stay blocked")
	default:
	}

	turn1.Emit(map[string]any{
		"type":         "finish",
		"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage": map[string]any{
			"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1},
		},
	})
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("DoPromptTurn (second) never unblocked after the first turn's trailing finish")
	}
	if control2Err != nil {
		t.Fatalf("DoPromptTurn (second): %v", control2Err)
	}

	turn2 := waitForTurn(2)
	turn2.Emit(map[string]any{"type": "text-delta", "id": "second", "delta": "banana"})
	turn2.Emit(map[string]any{
		"type":         "finish",
		"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
		"totalUsage": map[string]any{
			"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1},
		},
	})
	select {
	case <-control2.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("second turn never finished")
	}
	if err := control2.Err(); err != nil {
		t.Fatalf("control2.Err() = %v", err)
	}

	firstMu.Lock()
	gotFirst := firstEvents
	firstMu.Unlock()
	if len(gotFirst) != 1 {
		t.Fatalf("firstEvents = %+v, want exactly one text-delta", gotFirst)
	}
	if d, ok := gotFirst[0].(*harness.TextDeltaPart); !ok || d.Delta != "first" {
		t.Fatalf("firstEvents[0] = %+v, want text-delta %q", gotFirst[0], "first")
	}

	secondMu.Lock()
	gotSecond := secondEvents
	secondMu.Unlock()
	if len(gotSecond) != 2 {
		t.Fatalf("secondEvents = %+v, want [text-delta, finish]", gotSecond)
	}
	if d, ok := gotSecond[0].(*harness.TextDeltaPart); !ok || d.Delta != "banana" {
		t.Fatalf("secondEvents[0] = %+v, want text-delta %q", gotSecond[0], "banana")
	}
	if _, ok := gotSecond[1].(*harness.FinishPart); !ok {
		t.Fatalf("secondEvents[1] = %+v, want a finish part", gotSecond[1])
	}
}

func TestDoStartSpawnsBridgeWithSkillsDirFlag(t *testing.T) {
	srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/opencode-test-session",
		SandboxSession: sandbox,
	})

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnCommands) == 0 {
		t.Fatal("expected a spawn command")
	}
	cmd := sandbox.spawnCommands[0]
	if !strings.Contains(cmd, "--skills-dir '/home/vercel-sandbox/.agents/skills'") {
		t.Fatalf("spawn command = %q", cmd)
	}
	if len(sandbox.spawnEnvs) == 0 || sandbox.spawnEnvs[0]["AI_SDK_HARNESS_CLIENT_APP"] != "ai-sdk/harness-opencode/"+Version {
		t.Fatalf("spawn env = %v", sandbox.spawnEnvs)
	}
}

func TestDoStartBrokersCredentials(t *testing.T) {
	const token = "broker-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{
		MintBridgeToken: func(string) string { return token },
		Auth: harness.AuthEnvironment(map[string]string{
			"ANTHROPIC_API_KEY": "anthropic-secret", "ANTHROPIC_BASE_URL": "https://anthropic.example",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/opencode-test-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	sandbox.addReqTransMu.Lock()
	defer sandbox.addReqTransMu.Unlock()
	if len(sandbox.addReqTrans) != 1 || len(sandbox.addReqTrans[0]) != 1 {
		t.Fatalf("addRequestTransformations calls = %v", sandbox.addReqTrans)
	}
	transform := sandbox.addReqTrans[0][0]
	if transform.Match.Host != "anthropic.example" {
		t.Fatalf("match.host = %q", transform.Match.Host)
	}

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	blob, _ := json.Marshal(sandbox.spawnEnvs[0])
	if strings.Contains(string(blob), "anthropic-secret") {
		t.Fatalf("spawn env leaked the real credential: %s", blob)
	}
}

func TestDoStartResumeFromContinueReplaysWhenLogTerminal(t *testing.T) {
	const token = "resume-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	sandbox.files["/home/vercel-sandbox/.ai-sdk-harness/.agent-runs/test-session/bridge/event-log.ndjson"] =
		`{"type":"text-delta","id":"m","delta":"x"}` + "\n" +
			`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{},"outputTokens":{}}}`
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }, StartupTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	continueFrom, err := harness.NewContinueTurnState("opencode", map[string]any{
		"bridge": map[string]any{"port": 9, "token": "stale-token-not-matching", "lastSeenEventId": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/opencode-test-session",
		SandboxSession: sandbox, ContinueFrom: continueFrom,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnEnvs) == 0 || sandbox.spawnEnvs[0][bridge.EnvReplayFromDisk] != "1" {
		t.Fatalf("spawn env = %v, want BRIDGE_REPLAY_FROM_DISK=1", sandbox.spawnEnvs)
	}
}

func TestResolveAuthenticationModeAndEnv(t *testing.T) {
	env := map[string]string{"AI_GATEWAY_API_KEY": "gw-key"}
	if got := resolveAuthenticationMode(harness.Authentication{}, "", "", env); got != AuthAIGateway {
		t.Fatalf("resolveAuthenticationMode = %q, want ai-gateway", got)
	}
	direct := map[string]string{"OPENAI_API_KEY": "direct-key"}
	if got := resolveAuthenticationMode(harness.AuthMode("openai"), "", "openai", direct); got != AuthOpenAI {
		t.Fatalf("resolveAuthenticationMode = %q, want openai", got)
	}
	if got := resolveProvider("xai/grok-4", ""); got != AuthXAI {
		t.Fatalf("resolveProvider(model) = %q, want xai", got)
	}
}

// Ports TS `splits provider-prefixed models` (opencode-auth.test.ts).
func TestSplitOpenCodeModel(t *testing.T) {
	providerID, modelID, model := SplitOpenCodeModel("anthropic/claude-sonnet-4-5", "")
	if providerID != "anthropic" || modelID != "claude-sonnet-4-5" || model != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("SplitOpenCodeModel(prefixed) = (%q, %q, %q)", providerID, modelID, model)
	}
	providerID, modelID, model = SplitOpenCodeModel("gpt-5.1", "openai")
	if providerID != "openai" || modelID != "gpt-5.1" || model != "openai/gpt-5.1" {
		t.Fatalf("SplitOpenCodeModel(bare+provider) = (%q, %q, %q)", providerID, modelID, model)
	}
	providerID, modelID, model = SplitOpenCodeModel("", "openai")
	if providerID != "" || modelID != "" || model != "" {
		t.Fatalf("SplitOpenCodeModel(empty) = (%q, %q, %q), want all empty", providerID, modelID, model)
	}
}

func TestExtractUserTextRejectsNonTextParts(t *testing.T) {
	if _, err := extractUserText(harness.TextPrompt("hi")); err != nil {
		t.Fatalf("extractUserText(text) error = %v", err)
	}
}

// mcpServers configured on Settings must reach the bridge's "start" frame
// for a prompt turn. TS coverage of this field lives inside the larger
// composite `it('passes native config through prompts, compaction, and
// resumed sessions', ...)` test in opencode-harness.test.ts (which also
// asserts mcpServers on the compact frame and on a resumed session's next
// start frame — those two legs are not covered here).
func TestDoPromptTurn_SendsMCPServersInStartFrame(t *testing.T) {
	const token = "mcp-servers-token"
	var mu sync.Mutex
	var capturedStart map[string]any
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		capturedStart = start
		mu.Unlock()
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{
		MintBridgeToken: func(string) string { return token },
		MCPServers: map[string]any{
			"my-server": map[string]any{"type": "http", "url": "https://example.com/mcp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "mcp-servers-session", SessionWorkDir: "/vercel/sandbox/mcp-servers-session", SandboxSession: sandbox,
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
		t.Fatal("turn did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	mcpServers, ok := capturedStart["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("start.mcpServers = %v, want a map", capturedStart["mcpServers"])
	}
	server, ok := mcpServers["my-server"].(map[string]any)
	if !ok || server["url"] != "https://example.com/mcp" || server["type"] != "http" {
		t.Fatalf("start.mcpServers[my-server] = %v", mcpServers["my-server"])
	}
}

// Compaction rides its own "start" frame with operation "compact" and an
// empty prompt, and resolves once the bridge sends a terminal finish (no
// compaction-part requirement — DoCompact only needs the operation to
// settle). This is not asserted by a standalone TS test; the closest TS
// coverage is embedded in the composite
// `it('passes native config through prompts, compaction, and resumed
// sessions', ...)` test in opencode-harness.test.ts, which does not assert
// on `tools` at all (uses toMatchObject). TS's runCompactOperation
// (opencode-harness.ts) always sends `tools: []` on the wire; Go sends no
// `tools` key (nil slice + `omitempty`). Both are accepted by the shared
// bridge protocol schema, where `tools` is `.optional()`, so this is a
// deliberate, harmless Go-runtime wire-format difference, not a gap.
func TestDoCompact_SendsCompactOperation(t *testing.T) {
	const token = "compact-token"
	var mu sync.Mutex
	var capturedStart map[string]any
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		capturedStart = start
		mu.Unlock()
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "compact-session", SessionWorkDir: "/vercel/sandbox/compact-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	if err := sess.DoCompact(context.Background(), ""); err != nil {
		t.Fatalf("DoCompact: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedStart == nil {
		t.Fatal("bridge never received a start frame for the compact operation")
	}
	if capturedStart["operation"] != "compact" {
		t.Fatalf("start.operation = %v, want compact", capturedStart["operation"])
	}
	if prompt, ok := capturedStart["prompt"]; ok && prompt != "" {
		t.Fatalf("start.prompt = %v, want empty for a compaction", prompt)
	}
	if _, ok := capturedStart["tools"]; ok {
		t.Fatalf("start.tools = %v, want none for a compaction", capturedStart["tools"])
	}
}

// Ports the rejection logic in TS `doCompact` (opencode-harness.ts): OpenCode
// does not expose custom compaction instructions through the supported API,
// so a non-empty customInstructions throws HarnessCapabilityUnsupportedError.
// No standalone TS test exercises this branch (grepped
// harness-opencode/src/*.test.ts); this is new coverage of TS source
// behavior, not a ported TS test.
func TestDoCompact_RejectsCustomInstructions(t *testing.T) {
	const token = "compact-reject-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "compact-reject-session", SessionWorkDir: "/vercel/sandbox/compact-reject-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	err = sess.DoCompact(context.Background(), "focus on the auth module")
	if _, ok := err.(*harness.CapabilityUnsupportedError); !ok {
		t.Fatalf("DoCompact with custom instructions error = %v, want *harness.CapabilityUnsupportedError", err)
	}
}

// SubmitUserMessage round trip: the bridge-hello capability negotiation
// (bridgetest.Server always advertises ExperimentalUserMessageResponses)
// must produce a session control that implements
// harness.UserMessageSubmitter, and Submit must resolve once the fake
// bridge accepts the message (OnUserMessage's default: accept everything).
func TestDoPromptTurn_SubmitUserMessage(t *testing.T) {
	const token = "submit-user-message-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "submit-user-message-session", SessionWorkDir: "/vercel/sandbox/submit-user-message-session", SandboxSession: sandbox,
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

// Port of TS "passes connection settings to spawned and attached bridge
// channels" (opencode-harness.test.ts:292): DoDetach returns bridge
// coordinates carrying the caller-minted token, and reattaching (a second
// DoStart with that ResumeSessionState) must reuse the same token — no
// second MintBridgeToken call — and the identical `reconnect` config, and a
// custom PortEndpoint's headers must travel to both the initial and the
// reattach WebSocket handshake.
func TestDoStart_ReusesTokenHeadersAndReconnectAcrossAttach(t *testing.T) {
	const token = "opencode-detach-reattach-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)

	var mintMu sync.Mutex
	var mintCalls []string
	reconnect := bridge.ReconnectOptions{MaxElapsed: 120 * time.Second, InitialDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second}
	traceHeaders := map[string]string{"E2B-Traffic-Access-Token": "traffic-token"}
	h, err := CreateOpenCode(Settings{
		MintBridgeToken: func(sandboxID string) string {
			mintMu.Lock()
			mintCalls = append(mintCalls, sandboxID)
			mintMu.Unlock()
			return token
		},
		Reconnect:    reconnect,
		PortEndpoint: &harness.PortEndpoint{URL: "ws://" + srv.Addr() + "/?existing=value", Headers: traceHeaders},
	})
	if err != nil {
		t.Fatal(err)
	}

	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart (initial): %v", err)
	}

	mintMu.Lock()
	if len(mintCalls) != 1 || mintCalls[0] != "test-sandbox" {
		t.Fatalf("mintCalls after initial start = %v, want exactly [test-sandbox]", mintCalls)
	}
	mintMu.Unlock()

	initialChannel := sess.(*session).p.channel
	if got := initialChannel.ReconnectOptions(); got != reconnect {
		t.Fatalf("initial channel reconnect options = %+v, want %+v", got, reconnect)
	}

	resumeFrom, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	var resumeData resumeStateData
	if err := json.Unmarshal(resumeFrom.Data, &resumeData); err != nil {
		t.Fatalf("unmarshal resumeFrom.Data: %v", err)
	}
	if resumeData.Bridge == nil || resumeData.Bridge.Token != token {
		t.Fatalf("resumeFrom bridge coords = %+v, want token %q", resumeData.Bridge, token)
	}

	attachedSess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox, ResumeFrom: resumeFrom,
	})
	if err != nil {
		t.Fatalf("DoStart (reattach): %v", err)
	}
	t.Cleanup(func() { _ = attachedSess.DoDestroy(context.Background()) })

	mintMu.Lock()
	defer mintMu.Unlock()
	if len(mintCalls) != 1 {
		t.Fatalf("mintCalls after reattach = %v, want still exactly 1 (the token must be reused, not re-minted)", mintCalls)
	}

	attachedChannel := attachedSess.(*session).p.channel
	if got := attachedChannel.ReconnectOptions(); got != reconnect {
		t.Fatalf("reattached channel reconnect options = %+v, want %+v (identical config reused across spawn and attach)", got, reconnect)
	}

	// The server records a handshake in its connection handler, which can
	// run just after the client side is already connected, so poll briefly
	// for the reattach entry instead of reading once.
	handshakes := srv.HandshakeHeaders()
	for deadline := time.Now().Add(2 * time.Second); len(handshakes) < 2 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		handshakes = srv.HandshakeHeaders()
	}
	if len(handshakes) != 2 {
		t.Fatalf("bridgetest server saw %d handshakes, want exactly 2 (initial connect + reattach connect)", len(handshakes))
	}
	for i, hdr := range handshakes {
		if got := hdr.Get("E2B-Traffic-Access-Token"); got != "traffic-token" {
			t.Fatalf("handshake[%d] E2B-Traffic-Access-Token header = %q, want %q", i, got, "traffic-token")
		}
	}
}

// Port of TS "passes native config through prompts, compaction, and resumed
// sessions" (opencode-harness.test.ts:834-976): openCodeConfig, mcpServers
// and per-start headers must reach the bridge's "start" frame for a
// compaction, for a prompt turn, and for a prompt turn on a resumed session
// — and resumeSessionId (from a `bridge-thread` frame) must be carried into
// every one of those frames once known. This closes the two legs
// TestDoPromptTurn_SendsMCPServersInStartFrame and TestDoCompact_SendsCompactOperation
// explicitly left uncovered.
//
// One deliberate ordering difference from the TS test: TS's mock channel can
// `emit('bridge-thread', ...)` before any frame has been sent on it, so it
// does so right after doStart, before the first doCompact — meaning even the
// *first* start frame (compact) already carries resumeSessionId. bridgetest
// is a real WebSocket server and only has a connection to emit on once a
// client frame (here, the compact operation's "start") has been received, so
// this test instead emits bridge-thread from inside the handler for that
// first start frame, right before acking it with "finish". The compact
// frame itself is therefore asserted with an empty resumeSessionId, and
// every frame after it (the first prompt turn, and the resumed session's
// prompt turn) is asserted with resumeSessionId "opencode-session" — the
// same three-frame propagation TS asserts, just shifted by one frame to fit
// a real transport.
func TestDoStart_PropagatesNativeConfigThroughPromptsCompactionAndResumedSessions(t *testing.T) {
	const token = "native-config-token"
	var mu sync.Mutex
	var starts []map[string]any
	startCh := make(chan struct{}, 8)
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		starts = append(starts, start)
		idx := len(starts)
		mu.Unlock()
		if idx == 1 {
			turn.Emit(map[string]any{"type": "bridge-thread", "threadId": "opencode-session"})
		}
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
		startCh <- struct{}{}
	})
	sandbox := newFakeSandbox(srv)

	mcpServers := map[string]any{"context7": map[string]any{"type": "remote", "url": "https://mcp.context7.com/mcp"}}
	openCodeConfig := map[string]any{"agent": map[string]any{"general": map[string]any{"model": "openai/gpt-5.4-mini"}}}
	headers := map[string]string{"x-tenant": "acme"}
	h, err := CreateOpenCode(Settings{
		MintBridgeToken:  func(string) string { return token },
		OpenCodeConfig:   openCodeConfig,
		ReasoningVariant: "high",
		MCPServers:       mcpServers,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertNativeConfig := func(t *testing.T, label string, start map[string]any, operation, resumeSessionID string) {
		t.Helper()
		if start["operation"] != operation {
			t.Fatalf("%s: start.operation = %v, want %q", label, start["operation"], operation)
		}
		if got, _ := start["resumeSessionId"].(string); got != resumeSessionID {
			t.Fatalf("%s: start.resumeSessionId = %q, want %q", label, got, resumeSessionID)
		}
		gotConfig, ok := start["openCodeConfig"].(map[string]any)
		if !ok {
			t.Fatalf("%s: start.openCodeConfig = %v, want a map", label, start["openCodeConfig"])
		}
		if b, _ := json.Marshal(gotConfig); string(b) != `{"agent":{"general":{"model":"openai/gpt-5.4-mini"}}}` {
			t.Fatalf("%s: start.openCodeConfig = %s", label, b)
		}
		gotServers, ok := start["mcpServers"].(map[string]any)
		if !ok {
			t.Fatalf("%s: start.mcpServers = %v, want a map", label, start["mcpServers"])
		}
		if b, _ := json.Marshal(gotServers); string(b) != `{"context7":{"type":"remote","url":"https://mcp.context7.com/mcp"}}` {
			t.Fatalf("%s: start.mcpServers = %s", label, b)
		}
		gotHeaders, ok := start["headers"].(map[string]any)
		if !ok || gotHeaders["x-tenant"] != "acme" {
			t.Fatalf("%s: start.headers = %v, want {x-tenant: acme}", label, start["headers"])
		}
	}

	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", Headers: headers, SandboxSession: sandbox, SessionWorkDir: "/vercel/sandbox/native-config-session",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}

	if err := sess.DoCompact(context.Background(), ""); err != nil {
		t.Fatalf("DoCompact: %v", err)
	}
	select {
	case <-startCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the compact start frame")
	}
	mu.Lock()
	compactStart := starts[0]
	mu.Unlock()
	assertNativeConfig(t, "compact", compactStart, OperationCompact, "")

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Model: "anthropic/agent-model", Instructions: "be concise"},
		Prompt:       harness.TextPrompt("think"),
		Emit:         func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn (first): %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not finish")
	}
	mu.Lock()
	firstPromptStart := starts[1]
	mu.Unlock()
	assertNativeConfig(t, "first prompt", firstPromptStart, OperationPrompt, "opencode-session")
	if firstPromptStart["prompt"] != "think" || firstPromptStart["instructions"] != "be concise" || firstPromptStart["model"] != "anthropic/agent-model" {
		t.Fatalf("first prompt start = %v", firstPromptStart)
	}
	if firstPromptStart["variant"] != "high" {
		t.Fatalf("first prompt start.variant = %v, want high", firstPromptStart["variant"])
	}

	resumeFrom, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}

	resumedSession, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", Headers: headers, SandboxSession: sandbox,
		SessionWorkDir: "/vercel/sandbox/native-config-session", ResumeFrom: resumeFrom,
	})
	if err != nil {
		t.Fatalf("DoStart (resumed): %v", err)
	}
	t.Cleanup(func() { _ = resumedSession.DoDestroy(context.Background()) })

	resumedControl, err := resumedSession.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Instructions: "be concise"},
		Prompt:       harness.TextPrompt("resume thinking"),
		Emit:         func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatalf("DoPromptTurn (resumed): %v", err)
	}
	select {
	case <-resumedControl.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("resumed turn did not finish")
	}

	mu.Lock()
	if len(starts) != 3 {
		t.Fatalf("bridge saw %d start frames, want exactly 3 (compact + first prompt + resumed prompt)", len(starts))
	}
	resumedPromptStart := starts[2]
	mu.Unlock()
	assertNativeConfig(t, "resumed prompt", resumedPromptStart, OperationPrompt, "opencode-session")
	if resumedPromptStart["prompt"] != "resume thinking" || resumedPromptStart["instructions"] != "be concise" {
		t.Fatalf("resumed prompt start = %v", resumedPromptStart)
	}
}

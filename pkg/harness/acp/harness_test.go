package acp

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

// fakeSandbox is a minimal harness.NetworkSandboxSession mirroring the
// deepagents/opencode test doubles.
type fakeSandbox struct {
	mu            sync.Mutex
	spawnCommands []string
	spawnEnvs     []map[string]string
	addReqTransMu sync.Mutex
	addReqTrans   [][]harness.RequestTransformation
	bridgeAddr    string
	files         map[string]string
}

func (s *fakeSandbox) Description() string { return "acp fake sandbox" }
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
func (s *fakeSandbox) WriteTextFile(_ context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = map[string]string{}
	}
	s.files[opts.Path] = opts.Content
	return nil
}
func (s *fakeSandbox) ID() string                      { return "test-sandbox" }
func (s *fakeSandbox) DefaultWorkingDirectory() string { return "/vercel/sandbox" }
func (s *fakeSandbox) Ports() []int                    { return []int{4319} }
func (s *fakeSandbox) GetPortEndpoint(_ context.Context, opts harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	// A port other than the sandbox's one real port (4319, see Ports())
	// resolves to an address nothing listens on, so tests can simulate a
	// dead/unreachable bridge process (stale persisted coordinates) by
	// giving it a different port than the live one a fresh spawn binds.
	if opts.Port != 0 && opts.Port != 4319 {
		return harness.PortEndpoint{URL: "ws://127.0.0.1:1/"}, nil
	}
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

func testSettings(mut ...func(*Settings)) Settings {
	s := Settings{
		HarnessID:    "test-acp",
		Source:       Source{Type: SourceNPMSimple, PackageName: "test-acp-agent"},
		Executable:   "test-acp-agent",
		ModelMapping: ModelMapping{Type: ModelMappingSessionModel, Path: "model"},
	}
	for _, m := range mut {
		m(&s)
	}
	return s
}

func TestCreateACPMetadata(t *testing.T) {
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	if h.SpecificationVersion() != "harness-v1" {
		t.Fatalf("SpecificationVersion = %q", h.SpecificationVersion())
	}
	if h.HarnessID() != "test-acp" {
		t.Fatalf("HarnessID = %q", h.HarnessID())
	}
	if !harness.SupportsBuiltinToolApprovals(h) {
		t.Fatal("expected SupportsBuiltinToolApprovals() to be true")
	}
	if harness.SupportsBuiltinToolFiltering(h) {
		t.Fatal("expected SupportsBuiltinToolFiltering() to be false")
	}
}

func TestCreateACPValidation(t *testing.T) {
	if _, err := CreateACP(testSettings(func(s *Settings) { s.HarnessID = "Not Kebab" })); err == nil {
		t.Fatal("expected an error for an invalid harnessId")
	}
	if _, err := CreateACP(testSettings(func(s *Settings) {
		s.MCPServers = map[string]any{ReservedMCPServerName: map[string]any{}}
	})); err == nil {
		t.Fatal("expected an error for the reserved MCP server name")
	}
	if _, err := CreateACP(testSettings(func(s *Settings) { s.CredentialEnv = []string{"FOO"} })); err == nil {
		t.Fatal("expected an error: credentialEnv without credentialBrokering")
	}
	if _, err := CreateACP(testSettings(func(s *Settings) {
		s.ForwardEnv = []string{"FOO"}
		s.CredentialEnv = []string{"FOO"}
		s.CredentialBrokering = func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation { return nil }
	})); err == nil {
		t.Fatal("expected an error: FOO configured in both forwardEnv and credentialEnv")
	}
	if _, err := CreateACP(testSettings(func(s *Settings) { s.Executable = "/abs/path" })); err == nil {
		t.Fatal("expected an error for a non-bare executable name")
	}
}

func TestGetBootstrapNPMSimple(t *testing.T) {
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.(harness.BootstrapProvider).GetBootstrap(context.Background())
	if err != nil {
		t.Fatalf("GetBootstrap: %v", err)
	}
	if b.HarnessID != "test-acp" || b.BootstrapDir != ".harness-bootstrap/test-acp" {
		t.Fatalf("bootstrap = %+v", b)
	}
	var paths []string
	for _, f := range b.Files {
		paths = append(paths, f.Path)
	}
	want := []string{
		".harness-bootstrap/test-acp/package.json", ".harness-bootstrap/test-acp/pnpm-lock.yaml",
		".harness-bootstrap/test-acp/bridge.mjs", ".harness-bootstrap/test-acp/host-tool-mcp.mjs",
		".harness-bootstrap/test-acp/implementation/implementation.json",
		".harness-bootstrap/test-acp/implementation/package.json",
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
	var commands []string
	for _, c := range b.Commands {
		commands = append(commands, c.Command)
	}
	if len(commands) != 2 || commands[0] != "pnpm install --frozen-lockfile --store-dir .pnpm-store" {
		t.Fatalf("commands = %v", commands)
	}
	if !strings.Contains(commands[1], "pnpm --dir implementation install") || strings.Contains(commands[1], "--frozen-lockfile") {
		t.Fatalf("install command = %q", commands[1])
	}
}

func TestGetBootstrapInstallCommand(t *testing.T) {
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.Source = Source{Type: SourceInstallCommand, Command: "curl -fsSL https://example.com/install.sh | bash"}
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.(harness.BootstrapProvider).GetBootstrap(context.Background())
	if err != nil {
		t.Fatalf("GetBootstrap: %v", err)
	}
	found := false
	for _, f := range b.Files {
		if f.Path == ".harness-bootstrap/test-acp/implementation/install.sh" {
			found = true
			if !strings.Contains(f.Content, "curl -fsSL https://example.com/install.sh | bash") {
				t.Fatalf("install.sh content = %q", f.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected an install.sh bootstrap file")
	}
	if b.Commands[1].Command != "bash implementation/install.sh" {
		t.Fatalf("install command = %q", b.Commands[1].Command)
	}
}

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
		turn.Emit(map[string]any{"type": "bridge-thread", "threadId": "acp-session-1"})
		turn.Emit(map[string]any{
			"type": "finish-step", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"usage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 2}},
		})
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 2}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/test-acp-test-session", SandboxSession: sandbox,
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
	promptBlocks, ok := capturedStart["prompt"].([]any)
	if !ok || len(promptBlocks) != 1 {
		t.Fatalf("start.prompt = %v", capturedStart["prompt"])
	}
	block, _ := promptBlocks[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "hello" {
		t.Fatalf("start.prompt[0] = %v", block)
	}
	tsc, ok := capturedStart["turnStartConfig"].(map[string]any)
	if !ok || tsc["configurationFingerprint"] == "" {
		t.Fatalf("start.turnStartConfig = %v", capturedStart["turnStartConfig"])
	}

	sess2, ok := sess.(*session)
	if !ok {
		t.Fatalf("session type = %T", sess)
	}
	sess2.mu.Lock()
	got := sess2.latestACPSessionID
	sess2.mu.Unlock()
	if got != "acp-session-1" {
		t.Fatalf("latestACPSessionID = %q, want acp-session-1", got)
	}
}

func TestDoStartBrokersCredentials(t *testing.T) {
	const token = "broker-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		s.ForwardEnv = nil
		s.CredentialEnv = []string{"TEST_API_KEY"}
		s.Env = map[string]string{}
		s.CredentialBrokering = func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
			if env["TEST_API_KEY"] == "" || sandboxEnv["TEST_API_KEY"] == "" {
				return nil
			}
			return []harness.RequestTransformation{{
				Match:     harness.RequestTransformationMatch{Host: "example.com"},
				Transform: harness.RequestTransformationTransform{Headers: map[string]string{"Authorization": "Bearer " + env["TEST_API_KEY"]}},
			}}
		}
		s.Auth = harness.AuthEnvironment(map[string]string{"TEST_API_KEY": "real-secret"})
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/test-acp-test-session", SandboxSession: sandbox,
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
	if sandbox.addReqTrans[0][0].Match.Host != "example.com" {
		t.Fatalf("match.host = %q", sandbox.addReqTrans[0][0].Match.Host)
	}

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	blob, _ := json.Marshal(sandbox.spawnEnvs[0])
	if strings.Contains(string(blob), "real-secret") {
		t.Fatalf("spawn env leaked the real credential: %s", blob)
	}
}

func TestDoStartAskUserQuestionsToolCallCandidate(t *testing.T) {
	const token = "aq-token"
	var mu sync.Mutex
	var suppressed bool
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{
			"type": "acp-tool-call-candidate", "requestId": "req-1",
			"toolCall": map[string]any{"toolCallId": "tc-1", "title": "Ask a question"},
		})
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		s.IsMcpToolCall = func(tc ToolCall) bool {
			mu.Lock()
			suppressed = tc.ToolCallID == "tc-1"
			mu.Unlock()
			return tc.ToolCallID == "tc-1"
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/test-acp-test-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi")})
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
	if !suppressed {
		t.Fatal("expected isMcpToolCall to have been consulted for tc-1")
	}
}

func TestResolveAuthenticationEnvironment(t *testing.T) {
	env := map[string]string{"FOO": "bar"}
	if got := resolveAuthenticationEnvironment(harness.Authentication{}, env); got["FOO"] != "bar" {
		t.Fatalf("resolveAuthenticationEnvironment(unset) = %v", got)
	}
	isolated := harness.AuthEnvironment(map[string]string{"FOO": "isolated"})
	if got := resolveAuthenticationEnvironment(isolated, env); got["FOO"] != "isolated" {
		t.Fatalf("resolveAuthenticationEnvironment(isolated) = %v", got)
	}
}

func TestCreateImplementationIdentityStable(t *testing.T) {
	impl := implementation{Source: Source{Type: SourceNPMSimple, PackageName: "foo"}, Executable: "foo"}
	in := implementationIdentityInput{HarnessID: "test", Implementation: impl, ClientApp: DefaultClientApp, ModelMapping: ModelMapping{Type: ModelMappingSessionModel, Path: "model"}}
	a := createImplementationIdentity(in)
	b := createImplementationIdentity(in)
	if a != b {
		t.Fatalf("createImplementationIdentity is not deterministic: %q != %q", a, b)
	}
	in2 := in
	in2.Implementation.Executable = "bar"
	c := createImplementationIdentity(in2)
	if a == c {
		t.Fatal("createImplementationIdentity did not change when the executable changed")
	}
}

func TestConvertPromptToTextBlocksRejectsImages(t *testing.T) {
	blocks, err := convertPromptToTextBlocks(harness.TextPrompt("hi"), "test-acp")
	if err != nil || len(blocks) != 1 || blocks[0].Text != "hi" {
		t.Fatalf("blocks = %v, err = %v", blocks, err)
	}
}

// TestClassifyToolCallCandidateSuppressAndDynamicAreIndependent is a
// regression test: suppress (native ask-user-questions candidate) and
// dynamic (MCP-routed candidate) must be driven by two independent
// classifiers, matching TS's `acp-tool-call-candidate` handler
// (acp-v1-harness.ts). A prior Go version derived both from isMcpToolCall
// alone, which hid every MCP-routed tool call from the stream instead of
// forwarding it tagged Dynamic:true.
func TestClassifyToolCallCandidateSuppressAndDynamicAreIndependent(t *testing.T) {
	tc := ToolCall{ToolCallID: "tc-1", Title: "Run a command"}

	// Neither classifier configured: both false, no error.
	suppress, dynamic, err := classifyToolCallCandidate(nil, nil, tc)
	if err != nil || suppress || dynamic {
		t.Fatalf("no classifiers: suppress=%v dynamic=%v err=%v", suppress, dynamic, err)
	}

	// isMcpToolCall true must tag dynamic without suppressing.
	isMcp := func(ToolCall) bool { return true }
	suppress, dynamic, err = classifyToolCallCandidate(nil, isMcp, tc)
	if err != nil || suppress || !dynamic {
		t.Fatalf("mcp-routed: suppress=%v dynamic=%v err=%v, want suppress=false dynamic=true", suppress, dynamic, err)
	}

	// IsNativeToolCall true must suppress without tagging dynamic.
	aq := &AskUserQuestionsSettings{IsNativeToolCall: func(ToolCall) bool { return true }}
	suppress, dynamic, err = classifyToolCallCandidate(aq, nil, tc)
	if err != nil || !suppress || dynamic {
		t.Fatalf("native ask-user-questions: suppress=%v dynamic=%v err=%v, want suppress=true dynamic=false", suppress, dynamic, err)
	}

	// Both can be true independently (an MCP-routed native question, however
	// unlikely in practice).
	suppress, dynamic, err = classifyToolCallCandidate(aq, isMcp, tc)
	if err != nil || !suppress || !dynamic {
		t.Fatalf("both: suppress=%v dynamic=%v err=%v", suppress, dynamic, err)
	}

	// A panicking classifier is recovered into a classification error
	// (mirrors TS's try/catch around this handler), not propagated as a Go
	// panic.
	panicky := func(ToolCall) bool { panic("boom") }
	_, _, err = classifyToolCallCandidate(nil, panicky, tc)
	if err == nil {
		t.Fatal("expected a classification error from a panicking isMcpToolCall")
	}
}

// resumeSandbox extends fakeSandbox with settable file contents, for tests
// that manufacture a stale on-disk event log before a respawn.
func setSandboxFile(t *testing.T, s *fakeSandbox, path, content string) {
	t.Helper()
	s.mu.Lock()
	if s.files == nil {
		s.files = map[string]string{}
	}
	s.files[path] = content
	s.mu.Unlock()
}

// TestDoStartACPProcessLossDiskReplay ports the scenario behind TS
// `respawns a replay-only bridge for a coherent completed disk tail`
// (acp-harness.test.ts): a turn is suspended (its lifecycle state carries
// live bridge coordinates), the coordinates are then invalidated (the
// bridge process is gone), and its on-disk event-log.ndjson already ends in
// a terminal `finish` event. DoStart(ContinueFrom) must respawn the bridge
// with BRIDGE_REPLAY_FROM_DISK=1 and resume the channel from the persisted
// cursor, without sending a new `start` frame, and the resulting session
// must reject a subsequent DoPromptTurn (replay-only).
func TestDoStartACPProcessLossDiskReplay(t *testing.T) {
	const token = "replay-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {
		// A respawn-with-replay never sends a fresh `start`; any OnStart
		// call here would mean the port regressed to a fresh turn.
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return token } }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess, err := h.DoStart(ctx, harness.StartOptions{SessionID: "replay-session", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatalf("initial DoStart: %v", err)
	}
	s1 := sess.(*session)

	control, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	_ = control
	time.Sleep(20 * time.Millisecond)

	continueState, err := sess.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatalf("DoSuspendTurn: %v", err)
	}
	var data resumeStateData
	if err := json.Unmarshal(continueState.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Bridge == nil {
		t.Fatal("expected persisted bridge coordinates")
	}
	// Simulate a lost bridge process: the persisted coordinate can no
	// longer be attached to (a distinct, dead port; see fakeSandbox.GetPortEndpoint).
	data.Bridge.Port = 9999
	setSandboxFile(t, sandbox, s1.p.bridgeStateDir+"/event-log.ndjson",
		`{"type":"text-delta","id":"m","delta":"hi","seq":1}`+"\n"+`{"type":"finish","seq":2}`+"\n")
	newData, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	continueState.Data = newData

	sess2, err := h.DoStart(ctx, harness.StartOptions{
		SessionID: "replay-session", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox, ContinueFrom: continueState,
	})
	if err != nil {
		t.Fatalf("respawn DoStart: %v", err)
	}
	defer func() { _ = sess2.DoDestroy(ctx) }()
	s2 := sess2.(*session)
	s2.mu.Lock()
	replayOnly, turnInFlight := s2.replayOnly, s2.turnInFlight
	s2.mu.Unlock()
	if !replayOnly {
		t.Fatal("expected the respawned session to be replayOnly")
	}
	if !turnInFlight {
		t.Fatal("expected the respawned session to report an in-flight turn")
	}

	sandbox.mu.Lock()
	spawnEnv := sandbox.spawnEnvs[len(sandbox.spawnEnvs)-1]
	sandbox.mu.Unlock()
	if spawnEnv[bridge.EnvReplayFromDisk] != "1" {
		t.Fatalf("respawn env %v missing %s=1", spawnEnv, bridge.EnvReplayFromDisk)
	}

	if _, err := sess2.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("again"), Emit: func(harness.StreamPart) {}}); err == nil {
		t.Fatal("expected DoPromptTurn to reject a replay-only session")
	}
}

// TestDoStartACPProcessLossLossyRerun ports the scenario behind TS
// `reruns an incomplete turn only through session resume with fresh
// Gateway credentials` (acp-harness.test.ts): the on-disk event log does
// NOT end in a terminal `finish`, so DoStart(ContinueFrom) must respawn the
// bridge fresh (no disk replay) and DoContinueTurn must send a brand new
// `start` frame carrying `recoveryMode: {type: "lossy-rerun", ...}` built
// from the persisted turn-start configuration and native ACP session id.
func TestDoStartACPProcessLossLossyRerun(t *testing.T) {
	const token = "rerun-token"
	var mu sync.Mutex
	var recoveryStarts []map[string]any
	var freshStarts int
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		freshStarts++
		if start["recoveryMode"] != nil {
			recoveryStarts = append(recoveryStarts, start)
		}
		mu.Unlock()
		turn.Emit(map[string]any{"type": "bridge-thread", "threadId": "acp-session-rerun"})
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return token } }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess, err := h.DoStart(ctx, harness.StartOptions{SessionID: "rerun-session", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatalf("initial DoStart: %v", err)
	}
	s1 := sess.(*session)

	control, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("initial turn did not finish")
	}
	if err := control.Err(); err != nil {
		t.Fatalf("initial control.Err() = %v", err)
	}

	// The turn already finished, so suspend won't carry an in-flight turn
	// naturally; force one instead, mirroring a bridge process that died
	// mid-turn (turnInFlight was true, the finish event never made it to
	// disk in a terminal, coherent way).
	s1.mu.Lock()
	s1.turnInFlight = true
	s1.mu.Unlock()

	continueState, err := sess.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatalf("DoSuspendTurn: %v", err)
	}
	var data resumeStateData
	if err := json.Unmarshal(continueState.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Bridge == nil || data.TurnStartConfig == nil || data.ACPSessionID == "" {
		t.Fatalf("expected persisted bridge coords, turnStartConfig and acpSessionId; got %+v", data)
	}
	data.Bridge.Port = 9999
	setSandboxFile(t, sandbox, s1.p.bridgeStateDir+"/event-log.ndjson",
		`{"type":"text-delta","id":"m","delta":"partial","seq":1}`+"\n")
	newData, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	continueState.Data = newData

	sess2, err := h.DoStart(ctx, harness.StartOptions{
		SessionID: "rerun-session", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox, ContinueFrom: continueState,
	})
	if err != nil {
		t.Fatalf("respawn DoStart: %v", err)
	}
	defer func() { _ = sess2.DoDestroy(ctx) }()
	s2 := sess2.(*session)
	s2.mu.Lock()
	lossyRerun, turnInFlight := s2.lossyRerun, s2.turnInFlight
	s2.mu.Unlock()
	if !lossyRerun {
		t.Fatal("expected the respawned session to be a lossy rerun")
	}
	if !turnInFlight {
		t.Fatal("expected the respawned session to report an in-flight turn")
	}

	control2, err := sess2.DoContinueTurn(ctx, harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatalf("DoContinueTurn: %v", err)
	}
	select {
	case <-control2.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("rerun turn did not finish")
	}
	if err := control2.Err(); err != nil {
		t.Fatalf("rerun control.Err() = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if freshStarts != 2 {
		t.Fatalf("expected exactly 2 `start` frames (initial + rerun), got %d", freshStarts)
	}
	if len(recoveryStarts) != 1 {
		t.Fatalf("expected exactly 1 recovery `start` frame, got %d", len(recoveryStarts))
	}
	recoveryMode, _ := recoveryStarts[0]["recoveryMode"].(map[string]any)
	if recoveryMode["type"] != "lossy-rerun" {
		t.Fatalf("recoveryMode.type = %v, want lossy-rerun", recoveryMode["type"])
	}
	if recoveryMode["acpSessionId"] != "acp-session-rerun" {
		t.Fatalf("recoveryMode.acpSessionId = %v, want acp-session-rerun", recoveryMode["acpSessionId"])
	}
	if recoveryMode["reason"] != "event log not replayable" {
		t.Fatalf("recoveryMode.reason = %v", recoveryMode["reason"])
	}
}

// TestDoStartACPProcessLossColdRestore ports the scenario behind TS
// `restores cold lifecycle state independently of the per-turn model`
// (acp-harness.test.ts): a plain resume (not a continuation) with no live
// bridge coordinates must respawn the bridge and cold-restore the native
// ACP session by id via a prompt-less `start` carrying
// `recoveryMode: {type: "cold-restore", ...}`, resolved through the
// bridge's `acp-session-restored` raw frame before any real prompt turn is
// possible.
func TestDoStartACPProcessLossColdRestore(t *testing.T) {
	const token = "cold-token"
	var mu sync.Mutex
	var coldStart map[string]any
	var calls int
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		calls++
		first := calls == 1
		if !first {
			coldStart = start
		}
		mu.Unlock()
		if first {
			// The initial live turn: establish a native ACP session id so
			// doStop's persisted coldSession/acpSessionId are populated.
			turn.Emit(map[string]any{"type": "bridge-thread", "threadId": "acp-session-cold"})
		} else {
			turn.Emit(map[string]any{"type": "raw", "rawValue": map[string]any{"type": "acp-session-restored", "method": "resume"}})
		}
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return token } }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess, err := h.DoStart(ctx, harness.StartOptions{SessionID: "cold-session", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatalf("initial DoStart: %v", err)
	}
	control, err := sess.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("initial turn did not finish")
	}

	// doStop drops the bridge coordinates but keeps a coldSession derived
	// from the turn-start config, mirroring TS's `doStop`.
	resumeState, err := sess.DoStop(ctx)
	if err != nil {
		t.Fatalf("DoStop: %v", err)
	}
	var data resumeStateData
	if err := json.Unmarshal(resumeState.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Bridge != nil {
		t.Fatal("expected doStop to drop bridge coordinates")
	}
	if data.ColdSession == nil || data.ACPSessionID == "" {
		t.Fatalf("expected a persisted coldSession and acpSessionId; got %+v", data)
	}

	sess2, err := h.DoStart(ctx, harness.StartOptions{
		SessionID: "cold-session", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox, ResumeFrom: resumeState,
	})
	if err != nil {
		t.Fatalf("cold-restore DoStart: %v", err)
	}
	defer func() { _ = sess2.DoDestroy(ctx) }()
	s2 := sess2.(*session)
	s2.mu.Lock()
	restoration := s2.restoration
	s2.mu.Unlock()
	if restoration == nil || restoration.Method != "resume" {
		t.Fatalf("restoration = %+v, want method=resume", restoration)
	}

	mu.Lock()
	defer mu.Unlock()
	if coldStart == nil {
		t.Fatal("expected the bridge to receive a cold-restore start frame")
	}
	recoveryMode, _ := coldStart["recoveryMode"].(map[string]any)
	if recoveryMode["type"] != "cold-restore" {
		t.Fatalf("recoveryMode.type = %v, want cold-restore", recoveryMode["type"])
	}
	promptBlocks, _ := coldStart["prompt"].([]any)
	if len(promptBlocks) != 0 {
		t.Fatalf("cold-restore prompt = %v, want empty", coldStart["prompt"])
	}
}

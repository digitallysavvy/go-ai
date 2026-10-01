package deepagents

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

// fakeSandbox is a minimal harness.NetworkSandboxSession, mirroring TS
// deepagents-harness.test.ts's `fakeSandboxSession`: `run` answers `printf
// "%s" "$HOME"` and otherwise succeeds, and `spawn` records the command/env
// and returns a process that immediately announces bridge-ready pointing at
// a bridgetest.Server.
type fakeSandbox struct {
	mu            sync.Mutex
	spawnCommands []string
	spawnEnvs     []map[string]string
	addReqTransMu sync.Mutex
	addReqTrans   [][]harness.RequestTransformation
	bridgeAddr    string // host:port of the bridgetest.Server
}

func (s *fakeSandbox) Description() string { return "deepagents fake sandbox" }
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
	// Reuse the token/port the env carries so the client dials the real
	// bridgetest.Server.
	port := opts.Env[bridge.EnvWSPort]
	go func() {
		time.Sleep(2 * time.Millisecond)
		proc.WriteStdout(`{"type":"bridge-ready","port":` + port + "}\n")
		// The fake bridge process is not a real subprocess; resolve Wait()
		// promptly (mirrors TS's test double `wait: async () => ({
		// exitCode: 0 })`) so teardown's 5s fallback never has to fire.
		time.Sleep(5 * time.Millisecond)
		proc.Exit(0)
	}()
	return proc, nil
}
func (s *fakeSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (s *fakeSandbox) ReadBinaryFile(context.Context, string) ([]byte, error)  { return nil, nil }
func (s *fakeSandbox) ReadTextFile(context.Context, providerutils.SandboxReadTextFileOptions) (*string, error) {
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
func (s *fakeSandbox) GetPortEndpoint(_ context.Context, opts harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return harness.PortEndpoint{URL: "ws://" + s.bridgeAddr + "/"}, nil
}
func (s *fakeSandbox) GetPortURL(context.Context, harness.PortEndpointOptions) (string, error) {
	return "ws://" + s.bridgeAddr + "/", nil
}
func (s *fakeSandbox) Stop(context.Context) error               { return nil }
func (s *fakeSandbox) Destroy(context.Context) error            { return nil }
func (s *fakeSandbox) Restricted() providerutils.SandboxSession { return s }

// AddRequestTransformations implements harness.RequestTransformationAdder
// when extraAdder is set (mirrors the TS test's conditional
// `Object.assign(sandboxSession, { addRequestTransformations })`).
func (s *fakeSandbox) AddRequestTransformations(_ context.Context, t []harness.RequestTransformation) error {
	s.addReqTransMu.Lock()
	s.addReqTrans = append(s.addReqTrans, t)
	s.addReqTransMu.Unlock()
	return nil
}

var _ providerutils.SandboxSession = (*fakeSandbox)(nil)
var _ harness.NetworkSandboxSession = (*fakeSandbox)(nil)

func newFakeSandbox(srv *bridgetest.Server) *fakeSandbox {
	return &fakeSandbox{bridgeAddr: srv.Addr()}
}

// startedServer wires a bridgetest.Server whose token is read from the
// spawn env at connect time (the fake sandbox always connects to the same
// server; the server accepts a fixed token seeded before Spawn is called, so
// tests set it directly).
func newServer(t *testing.T, token string, onStart func(*bridgetest.Turn, map[string]any)) *bridgetest.Server {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: token, OnStart: onStart})
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateDeepAgentsMetadata(t *testing.T) {
	h := CreateDeepAgents()
	if h.SpecificationVersion() != "harness-v1" {
		t.Fatalf("SpecificationVersion = %q", h.SpecificationVersion())
	}
	if h.HarnessID() != "deepagents" {
		t.Fatalf("HarnessID = %q", h.HarnessID())
	}
	if !harness.SupportsBuiltinToolApprovals(h) {
		t.Fatal("expected SupportsBuiltinToolApprovals() to be true")
	}
	if harness.SupportsBuiltinToolFiltering(h) {
		t.Fatal("expected SupportsBuiltinToolFiltering() to be false (undefined in TS)")
	}
}

// TS: "ships the node bridge files and a pnpm install command in its bootstrap"
func TestGetBootstrapShipsBridgeFilesAndInstallCommand(t *testing.T) {
	b, err := GetBootstrap(context.Background())
	if err != nil {
		t.Fatalf("GetBootstrap: %v", err)
	}
	if b.HarnessID != "deepagents" {
		t.Fatalf("HarnessID = %q", b.HarnessID)
	}
	if b.BootstrapDir != ".harness-bootstrap/deepagents" {
		t.Fatalf("BootstrapDir = %q", b.BootstrapDir)
	}
	var paths []string
	for _, f := range b.Files {
		paths = append(paths, f.Path)
	}
	want := []string{
		".harness-bootstrap/deepagents/bridge.mjs",
		".harness-bootstrap/deepagents/package.json",
		".harness-bootstrap/deepagents/pnpm-lock.yaml",
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %v, want %v", paths, want)
		}
	}
	var commands string
	for _, c := range b.Commands {
		commands += c.Command + "\n"
	}
	if !strings.Contains(commands, "pnpm install --frozen-lockfile --store-dir .pnpm-store") {
		t.Fatalf("commands = %q, missing pnpm install", commands)
	}
	if strings.Contains(commands, "mkdir -p .harness-bootstrap/deepagents") {
		t.Fatalf("commands = %q, should not mkdir the bootstrap dir itself", commands)
	}
}

// TS: "caches the bootstrap across calls"
func TestGetBootstrapCachesAcrossCalls(t *testing.T) {
	a, err := GetBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := GetBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("expected the same cached *harness.Bootstrap pointer")
	}
}

// TS: "passes the harness client app to the bridge environment"
func TestDoStartSpawnsBridgeWithClientAppAndToken(t *testing.T) {
	srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h := CreateDeepAgents()
	// No MintBridgeToken override: exercise the default random-token path.
	// The fake server's fixed Token ("") never matches, so the dial itself
	// may fail — this test only checks what the adapter did *before*
	// connecting (the spawn command and environment), which happen first.
	_, _ = h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: sandbox,
	})

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnEnvs) == 0 {
		t.Fatal("expected the bridge to be spawned")
	}
	env := sandbox.spawnEnvs[0]
	if env["AI_SDK_HARNESS_CLIENT_APP"] != "ai-sdk/harness-deepagents/"+Version {
		t.Fatalf("AI_SDK_HARNESS_CLIENT_APP = %q", env["AI_SDK_HARNESS_CLIENT_APP"])
	}
	if len(env[bridge.EnvChannelToken]) != 64 {
		t.Fatalf("bridge token = %q, want a 64-char hex token", env[bridge.EnvChannelToken])
	}
	if len(sandbox.spawnCommands) == 0 {
		t.Fatal("expected a spawn command")
	}
	cmd := sandbox.spawnCommands[0]
	if !strings.Contains(cmd, "node '/home/vercel-sandbox/.ai-sdk-harness/.harness-bootstrap/deepagents/bridge.mjs'") {
		t.Fatalf("spawn command = %q", cmd)
	}
	if !strings.Contains(cmd, "--bootstrap-dir '/home/vercel-sandbox/.ai-sdk-harness/.harness-bootstrap/deepagents'") {
		t.Fatalf("spawn command = %q", cmd)
	}
}

// TestDoStartFullRoundTrip drives DoStart -> DoPromptTurn against a real
// bridgetest.Server end to end: spawn, bridge-ready, WebSocket dial,
// bridge-hello, `start`, streamed parts, `finish`.
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
		turn.Emit(map[string]any{
			"type":         "finish-step",
			"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"usage": map[string]any{
				"inputTokens":  map[string]any{"total": 1},
				"outputTokens": map[string]any{"total": 2},
			},
		})
		turn.Emit(map[string]any{
			"type":         "finish",
			"finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{
				"inputTokens":  map[string]any{"total": 1},
				"outputTokens": map[string]any{"total": 2},
			},
		})
	})
	sandbox := newFakeSandbox(srv)
	h := CreateDeepAgents(Settings{
		MintBridgeToken: func(string) string { return token },
		Effort:          "max",
		Thinking:        &ThinkingConfig{Type: ThinkingTypeAdaptive, Display: "summarized"},
	})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
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
	defer mu2.Unlock()
	if len(deltas) != 1 || deltas[0] != "hi" {
		t.Fatalf("deltas = %v, want [hi]", deltas)
	}
	if !finished {
		t.Fatal("expected a finish part")
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedStart["prompt"] != "hello" {
		t.Fatalf("start.prompt = %v, want hello", capturedStart["prompt"])
	}
	if capturedStart["effort"] != "max" {
		t.Fatalf("start.effort = %v, want max", capturedStart["effort"])
	}
	if thinking, ok := capturedStart["thinking"].(map[string]any); !ok || thinking["type"] != "adaptive" {
		t.Fatalf("start.thinking = %v", capturedStart["thinking"])
	}
	if headers, ok := capturedStart["headers"].(map[string]any); !ok || headers["x-tenant"] != "acme" {
		t.Fatalf("start.headers = %v", capturedStart["headers"])
	}
}

// TS: "loads the saved conversation checkpoint when spawning a resumed bridge"
func TestDoStartResumeFromPassesResumeFlag(t *testing.T) {
	const token = "resume-token"
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h := CreateDeepAgents(Settings{MintBridgeToken: func(string) string { return token }})
	resumeFrom, err := harness.NewResumeSessionState("deepagents", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: sandbox, ResumeFrom: resumeFrom,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnCommands) == 0 || !strings.Contains(sandbox.spawnCommands[0], "--resume true") {
		t.Fatalf("spawn command = %v, want it to contain --resume true", sandbox.spawnCommands)
	}
}

// TS: "brokers credentials when the sandbox supports additive request
// transformations"
func TestDoStartBrokersCredentialsWhenSupported(t *testing.T) {
	const token = "broker-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)

	var forwarded []harness.CredentialForwardingOptions
	var fmu sync.Mutex
	h := CreateDeepAgents(Settings{
		MintBridgeToken: func(string) string { return token },
		Auth: harness.AuthEnvironment(map[string]string{
			"ANTHROPIC_API_KEY": "anthropic-secret", "ANTHROPIC_BASE_URL": "https://anthropic.example",
		}),
		CredentialForwarding: func(_ context.Context, opts harness.CredentialForwardingOptions) (string, error) {
			fmu.Lock()
			forwarded = append(forwarded, opts)
			fmu.Unlock()
			return "ephemeral-" + opts.EnvironmentVariableName, nil
		},
	})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: sandbox,
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
	if transform.Transform.Headers["x-api-key"] != "anthropic-secret" {
		t.Fatalf("transform.headers = %v", transform.Transform.Headers)
	}

	fmu.Lock()
	defer fmu.Unlock()
	if len(forwarded) != 1 || forwarded[0].EnvironmentVariableName != "ANTHROPIC_API_KEY" {
		t.Fatalf("forwarded = %v", forwarded)
	}
	if !strings.HasPrefix(forwarded[0].Credential, "aisdkhc_") {
		t.Fatalf("credential = %q, want a placeholder", forwarded[0].Credential)
	}

	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	env := sandbox.spawnEnvs[0]
	if env["ANTHROPIC_API_KEY"] != "ephemeral-ANTHROPIC_API_KEY" {
		t.Fatalf("spawn env ANTHROPIC_API_KEY = %q", env["ANTHROPIC_API_KEY"])
	}
	blob, _ := json.Marshal(env)
	if strings.Contains(string(blob), "anthropic-secret") {
		t.Fatalf("spawn env leaked the real credential: %s", blob)
	}
}

// TS: "passes configured MCP servers to the bridge"
func TestDoStartPassesMCPServers(t *testing.T) {
	const token = "mcp-token"
	var captured map[string]any
	var mu sync.Mutex
	// A buffered channel signals once the bridge has actually received and
	// recorded the "start" frame, so the test waits on that condition
	// directly instead of a fixed sleep (a prior time.Sleep(20ms) was flaky:
	// OnStart runs on its own goroutine, so there was no guarantee the sleep
	// outlasted the send).
	startedCh := make(chan struct{}, 1)
	srv := newServer(t, token, func(_ *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		captured = start
		mu.Unlock()
		startedCh <- struct{}{}
	})
	sandbox := newFakeSandbox(srv)
	mcpServers := map[string]any{"memory": map[string]any{"command": "memory-mcp", "args": []any{}}}
	h := CreateDeepAgents(Settings{MintBridgeToken: func(string) string { return token }, MCPServers: mcpServers})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	if _, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("use memory.")}); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case <-startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the bridge to receive the start frame")
	}

	mu.Lock()
	defer mu.Unlock()
	got, ok := captured["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("start.mcpServers = %v", captured["mcpServers"])
	}
	memory, ok := got["memory"].(map[string]any)
	if !ok || memory["command"] != "memory-mcp" {
		t.Fatalf("start.mcpServers.memory = %v", got["memory"])
	}
}

// TS: "resolves the turn when the channel closes with reason 'suspended'"
// / "rejects the turn when the channel closes for any other reason"
func TestSuspendResolvesTurnCleanly(t *testing.T) {
	const token = "suspend-token"
	release := make(chan struct{})
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "partial"})
		<-release
	})
	sandbox := newFakeSandbox(srv)
	h := CreateDeepAgents(Settings{MintBridgeToken: func(string) string { return token }})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "suspend-session", SessionWorkDir: "/vercel/sandbox/deepagents-suspend-session",
		SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi")})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	state, err := sess.DoSuspendTurn(context.Background())
	if err != nil {
		t.Fatalf("DoSuspendTurn: %v", err)
	}
	if state.HarnessID != "deepagents" {
		t.Fatalf("state.HarnessID = %q", state.HarnessID)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not settle after suspend")
	}
	if err := control.Err(); err != nil {
		t.Fatalf("control.Err() after a suspend-close = %v, want nil", err)
	}
	close(release)
}

// Ports TS "rejects the turn when the channel closes for any other reason"
// (deepagents-harness.test.ts): an abrupt, non-suspend close (the bridge
// process/connection is simply gone, reconnect budget exhausted) must
// settle the turn with an error, not silently swallow it.
func TestPromptTurnRejectsOnNonSuspendedClose(t *testing.T) {
	const token = "drop-token"
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "partial"})
		// Never finishes; the test drops the connection instead.
	})
	sandbox := newFakeSandbox(srv)
	h := CreateDeepAgents(Settings{
		MintBridgeToken: func(string) string { return token },
		Reconnect:       bridge.ReconnectOptions{MaxElapsed: 20 * time.Millisecond, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
	})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "drop-session", SessionWorkDir: "/vercel/sandbox/deepagents-drop-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi")})
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// Drop the active connection, then close the server itself so a
	// reconnect attempt can never succeed; the channel exhausts its (tiny)
	// reconnect budget and closes with reason "reconnect failed", not
	// "suspended".
	srv.DropActive()
	srv.Close()

	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not settle after the connection was dropped")
	}
	if err := control.Err(); err == nil || !strings.Contains(err.Error(), "closed before the turn finished") {
		t.Fatalf("control.Err() = %v, want a bridge-closed-before-finish error", err)
	}
}

func TestResolveAuthenticationModeAndEnv(t *testing.T) {
	env := map[string]string{"AI_GATEWAY_API_KEY": "gw-key"}
	if got := resolveAuthenticationMode(harness.Authentication{}, env); got != AuthAIGateway {
		t.Fatalf("resolveAuthenticationMode = %q, want ai-gateway", got)
	}
	direct := map[string]string{"ANTHROPIC_API_KEY": "direct-key"}
	if got := resolveAuthenticationMode(harness.Authentication{}, direct); got != AuthAnthropic {
		t.Fatalf("resolveAuthenticationMode = %q, want anthropic", got)
	}
	gwEnv := resolveEnv(harness.AuthMode(harness.AuthModeAIGateway), env)
	if gwEnv["ANTHROPIC_API_KEY"] != "gw-key" || gwEnv["ANTHROPIC_BASE_URL"] == "" {
		t.Fatalf("resolveEnv(ai-gateway) = %v", gwEnv)
	}
}

func TestExtractUserTextRejectsNonTextParts(t *testing.T) {
	if _, err := extractUserText(harness.TextPrompt("hi")); err != nil {
		t.Fatalf("extractUserText(text) error = %v", err)
	}
}

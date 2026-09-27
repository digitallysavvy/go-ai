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

func TestExtractUserTextRejectsNonTextParts(t *testing.T) {
	if _, err := extractUserText(harness.TextPrompt("hi")); err != nil {
		t.Fatalf("extractUserText(text) error = %v", err)
	}
}

package githubcopilot

import (
	"context"
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

// e2eFakeSandbox is a minimal harness.NetworkSandboxSession that spawns the
// bridge process by writing a bridge-ready frame to stdout, mirroring
// pkg/harness/acp's own test double. It exists per-package (rather than
// shared) because it is a small test-only fixture and pkg/harness/acp's
// equivalent is unexported.
type e2eFakeSandbox struct {
	mu         sync.Mutex
	files      map[string]string
	bridgeAddr string
	spawnCmds  []string
}

func newE2EFakeSandbox(srv *bridgetest.Server) *e2eFakeSandbox {
	return &e2eFakeSandbox{files: map[string]string{}, bridgeAddr: srv.Addr()}
}

func (s *e2eFakeSandbox) Description() string { return "github-copilot e2e fake sandbox" }
func (s *e2eFakeSandbox) Run(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if opts.Command == `printf "%s" "$HOME"` {
		return providerutils.SandboxRunResult{ExitCode: 0, Stdout: "/home/vercel-sandbox"}, nil
	}
	return providerutils.SandboxRunResult{ExitCode: 0}, nil
}
func (s *e2eFakeSandbox) Spawn(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	s.mu.Lock()
	s.spawnCmds = append(s.spawnCmds, opts.Command)
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
func (s *e2eFakeSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (s *e2eFakeSandbox) ReadBinaryFile(context.Context, string) ([]byte, error)  { return nil, nil }
func (s *e2eFakeSandbox) ReadTextFile(_ context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.files[opts.Path]; ok {
		return &v, nil
	}
	return nil, nil
}
func (s *e2eFakeSandbox) WriteFile(context.Context, string, io.Reader) error    { return nil }
func (s *e2eFakeSandbox) WriteBinaryFile(context.Context, string, []byte) error { return nil }
func (s *e2eFakeSandbox) WriteTextFile(_ context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = map[string]string{}
	}
	s.files[opts.Path] = opts.Content
	return nil
}
func (s *e2eFakeSandbox) ID() string                      { return "github-copilot-e2e-sandbox" }
func (s *e2eFakeSandbox) DefaultWorkingDirectory() string { return "/vercel/sandbox" }
func (s *e2eFakeSandbox) Ports() []int                    { return []int{4319} }
func (s *e2eFakeSandbox) GetPortEndpoint(_ context.Context, _ harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return harness.PortEndpoint{URL: "ws://" + s.bridgeAddr + "/"}, nil
}
func (s *e2eFakeSandbox) GetPortURL(context.Context, harness.PortEndpointOptions) (string, error) {
	return "ws://" + s.bridgeAddr + "/", nil
}
func (s *e2eFakeSandbox) Stop(context.Context) error               { return nil }
func (s *e2eFakeSandbox) Destroy(context.Context) error            { return nil }
func (s *e2eFakeSandbox) Restricted() providerutils.SandboxSession { return s }
func (s *e2eFakeSandbox) AddRequestTransformations(context.Context, []harness.RequestTransformation) error {
	return nil
}

var _ harness.NetworkSandboxSession = (*e2eFakeSandbox)(nil)

// TestCreateGitHubCopilot_E2ERoundTrip drives CreateGitHubCopilot's
// harness.Harness through a fake ACP bridge end to end: DoStart launches the
// bridge process (spawning the exact `copilot --acp --stdio
// --no-auto-update` command BuildConfig configures), and a prompt turn
// streams a text delta back through the real ACP session/channel machinery.
func TestCreateGitHubCopilot_E2ERoundTrip(t *testing.T) {
	const token = "github-copilot-e2e-token"
	srv := bridgetest.NewServer(bridgetest.Options{
		Token: token,
		OnStart: func(turn *bridgetest.Turn, start map[string]any) {
			turn.Emit(map[string]any{"type": "text-start", "id": "m"})
			turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "hello from copilot"})
			turn.Emit(map[string]any{"type": "text-end", "id": "m"})
			turn.Emit(map[string]any{
				"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
				"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
			})
		},
	})
	defer srv.Close()
	sandbox := newE2EFakeSandbox(srv)

	port := 4319
	h, err := CreateGitHubCopilot(Settings{
		Port:            &port,
		PortEndpoint:    &harness.PortEndpoint{URL: "ws://" + sandbox.bridgeAddr + "/"},
		MintBridgeToken: func(string) string { return token },
	})
	if err != nil {
		t.Fatalf("CreateGitHubCopilot: %v", err)
	}
	if h.HarnessID() != "github-copilot" {
		t.Fatalf("HarnessID() = %q, want github-copilot", h.HarnessID())
	}

	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "e2e-session", SessionWorkDir: "/vercel/sandbox/github-copilot-e2e-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()

	sandbox.mu.Lock()
	spawned := append([]string(nil), sandbox.spawnCmds...)
	sandbox.mu.Unlock()
	if len(spawned) == 0 || !strings.Contains(spawned[0], "copilot") {
		t.Fatalf("spawned commands = %v, want the copilot bridge command", spawned)
	}

	var deltas []string
	var finished bool
	var mu sync.Mutex
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"),
		Emit: func(p harness.StreamPart) {
			mu.Lock()
			defer mu.Unlock()
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
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}
	if err := control.Err(); err != nil {
		t.Fatalf("control.Err() = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deltas) != 1 || deltas[0] != "hello from copilot" {
		t.Fatalf("deltas = %v", deltas)
	}
	if !finished {
		t.Fatal("expected a finish part")
	}
}

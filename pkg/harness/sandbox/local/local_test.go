package local

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewProvider(Options{RootDir: root, Env: map[string]string{"LOCAL_SANDBOX_TEST": "1"}})
}

func TestSessionRunUsesWorkDirAndIsolatedHome(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider(t)
	s, err := p.CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "s/1"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy(ctx) //nolint:errcheck

	if s.ID() != "s/1" || !strings.HasSuffix(s.DefaultWorkingDirectory(), "/s%2F1/work") {
		t.Fatalf("id=%s wd=%s", s.ID(), s.DefaultWorkingDirectory())
	}
	res, err := s.Run(ctx, providerutils.SandboxProcessOptions{Command: `pwd; printf "%s|%s|%s" "$HOME" "$LOCAL_SANDBOX_TEST" "$X"`, Env: map[string]string{"X": "y"}})
	if err != nil || res.ExitCode != 0 {
		t.Fatal(res, err)
	}
	lines := strings.SplitN(res.Stdout, "\n", 2)
	if lines[0] != s.DefaultWorkingDirectory() {
		t.Fatalf("pwd = %q", lines[0])
	}
	home, err := harness.ResolveSandboxHomeDir(ctx, s)
	if err != nil || lines[1] != home+"|1|y" || !strings.HasSuffix(home, "/s%2F1/home") {
		t.Fatalf("env = %q home=%q err=%v", lines[1], home, err)
	}

	res, _ = s.Run(ctx, providerutils.SandboxProcessOptions{Command: "echo err >&2; exit 3"})
	if res.ExitCode != 3 || res.Stderr != "err\n" {
		t.Fatalf("%+v", res)
	}
}

func TestSessionFilesResolveRelativeToWorkDir(t *testing.T) {
	ctx := context.Background()
	s, err := newTestProvider(t).CreateSession(ctx, harness.CreateSandboxSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy(ctx) //nolint:errcheck
	if !strings.HasPrefix(s.ID(), "local-") {
		t.Fatal(s.ID())
	}
	if err := s.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: "a/b.txt", Content: "l1\nl2\nl3"}); err != nil {
		t.Fatal(err)
	}
	two := 2
	got, err := s.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: filepath.Join(s.DefaultWorkingDirectory(), "a/b.txt"), StartLine: &two})
	if err != nil || got == nil || *got != "l2\nl3" {
		t.Fatal(got, err)
	}
	missing, err := s.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: "nope"})
	if err != nil || missing != nil {
		t.Fatal("missing files read as nil")
	}
}

func TestPortsAndEndpoints(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestProvider(t).CreateSession(ctx, harness.CreateSandboxSessionOptions{})
	defer s.Destroy(ctx) //nolint:errcheck
	ep, err := s.GetPortEndpoint(ctx, harness.PortEndpointOptions{Port: 4000, Protocol: harness.PortProtocolWS})
	if err != nil || ep.URL != "ws://127.0.0.1:4000" {
		t.Fatal(ep, err)
	}
	url, _ := s.GetPortURL(ctx, harness.PortEndpointOptions{Port: 4000})
	if url != "http://127.0.0.1:4000" {
		t.Fatal(url)
	}
	if _, err := s.GetPortEndpoint(ctx, harness.PortEndpointOptions{Port: 0}); err == nil {
		t.Fatal("invalid port")
	}
	_ = s.(harness.PortsSetter).SetPorts(ctx, []int{9, 3})
	if got := s.Ports(); len(got) != 2 || got[0] != 3 {
		t.Fatal(got)
	}
}

func TestStopKillsProcessGroupAndDestroyRemovesFiles(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider(t)
	s, _ := p.CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "kill"})
	// Mimic a bridge: announce readiness on stdout, then keep a child alive.
	proc, err := s.Spawn(ctx, providerutils.SandboxProcessOptions{Command: `echo '{"type":"bridge-ready","port":1}'; sleep 30 & wait`})
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != `{"type":"bridge-ready","port":1}` {
		t.Fatal(line, err)
	}
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not kill the process")
	}
	if _, err := s.Spawn(ctx, providerutils.SandboxProcessOptions{Command: "true"}); err == nil {
		t.Fatal("stopped sessions reject new processes")
	}

	resumed, err := p.ResumeSession(ctx, "kill")
	if err != nil || resumed.ID() != "kill" {
		t.Fatal(resumed, err)
	}
	dir := filepath.Dir(s.DefaultWorkingDirectory())
	if err := s.Destroy(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("destroy must remove the session directory")
	}
	if _, err := p.ResumeSession(ctx, "kill"); err == nil {
		t.Fatal("resume of a destroyed session must fail")
	}
}

func TestContextCancellationKillsProcess(t *testing.T) {
	s, _ := newTestProvider(t).CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	defer s.Destroy(context.Background()) //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.Run(ctx, providerutils.SandboxProcessOptions{Command: "sleep 10"})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatal("expected cancellation", err)
	}
}

type recipeHarness struct{ recipe *harness.Bootstrap }

func (h recipeHarness) SpecificationVersion() string                  { return harness.SpecificationVersion }
func (h recipeHarness) HarnessID() string                             { return "demo" }
func (h recipeHarness) BuiltinTools() map[string]harness.BuiltinTool { return nil }
func (h recipeHarness) DoStart(context.Context, harness.StartOptions) (harness.Session, error) {
	return nil, nil
}
func (h recipeHarness) GetBootstrap(context.Context) (*harness.Bootstrap, error) {
	return h.recipe, nil
}

// End to end: bootstrap files and marker land under $HOME/.ai-sdk-harness,
// never under the working directory (9c8c0c1), and re-running is a no-op.
func TestBootstrapEndToEnd(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider(t)
	recipe := &harness.Bootstrap{
		HarnessID:    "demo",
		BootstrapDir: ".harness-bootstrap/demo",
		Files:        []harness.BootstrapFile{{Path: ".harness-bootstrap/demo/bridge.mjs", Content: "console.log(1)"}},
		Commands:     []harness.BootstrapCommand{{Command: "echo ran >> runs.log"}},
	}
	s, _ := p.CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "boot"})
	defer s.Destroy(ctx) //nolint:errcheck
	for i := 0; i < 2; i++ {
		result, err := harness.PrepareSandboxForHarness(ctx, harness.PrepareSandboxForHarnessOptions{Session: s, Harnesses: []harness.Harness{recipeHarness{recipe}}})
		if err != nil {
			t.Fatal(err)
		}
		if result.RecipeIdentities["demo"] != harness.HashHarnessBootstrap(*recipe) {
			t.Fatal(result)
		}
	}
	home := s.(*Session).HomeDir()
	bootDir := filepath.Join(home, ".ai-sdk-harness", ".harness-bootstrap", "demo")
	if data, _ := os.ReadFile(filepath.Join(bootDir, "bridge.mjs")); string(data) != "console.log(1)" {
		t.Fatal("bridge file not written under HOME state dir")
	}
	if data, _ := os.ReadFile(filepath.Join(bootDir, "runs.log")); string(data) != "ran\n" {
		t.Fatalf("commands must run once from the bootstrap dir, got %q", data)
	}
	if _, err := os.Stat(filepath.Join(bootDir, ".bootstrap-"+harness.HashHarnessBootstrap(*recipe)+".ok")); err != nil {
		t.Fatal("marker missing")
	}
	entries, _ := os.ReadDir(s.DefaultWorkingDirectory())
	if len(entries) != 0 {
		t.Fatal("bootstrap must not touch the working directory")
	}
}

func TestOnFirstCreateRunsOnlyForFreshSessions(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider(t)
	calls := 0
	onFirst := func(context.Context, providerutils.SandboxSession) error { calls++; return nil }
	s, _ := p.CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "x", OnFirstCreate: onFirst})
	_, _ = p.CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "x", OnFirstCreate: onFirst})
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if _, ok := s.Restricted().(harness.NetworkSandboxSession); ok {
		t.Fatal("restricted view must not expose the network surface")
	}
	_ = s.Destroy(ctx)
}

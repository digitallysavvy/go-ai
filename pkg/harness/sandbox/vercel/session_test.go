package vercel

// Ports packages/sandbox-vercel/src/vercel-sandbox-session.test.ts
// (VercelSandboxSession) against the fake HTTP server in testserver_test.go
// instead of a mocked native `Sandbox` (Go has no such object to mock —
// see package doc in provider.go).

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func newTestSandbox(t *testing.T, f *fakeServer, name string) *Sandbox {
	t.Helper()
	client := NewAPIClient(f.URL(), Credentials{Token: "tok", TeamID: "team", ProjectID: "proj"})
	sbx, err := CreateSandbox(context.Background(), client, CreateParams{Name: name, Runtime: "node24"})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	return sbx
}

func TestSessionDescriptionMentionsSandboxName(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_test")
	desc := NewSession(sbx).Description()
	if !strings.Contains(desc, "sbx_test") {
		t.Fatalf("expected description to mention sbx_test, got %q", desc)
	}
}

func TestSessionRunWrapsInBashC(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	var gotCommand string
	var gotArgs []string
	f.CommandFn = func(_, command string, args []string, _ string, _ map[string]string) CommandScript {
		gotCommand, gotArgs = command, args
		return CommandScript{Stdout: "hi\n", Stderr: "oops\n", ExitCode: 0}
	}
	sbx := newTestSandbox(t, f, "sbx_run")

	result, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "echo hi"})
	if err != nil {
		t.Fatal(err)
	}
	if gotCommand != "bash" || len(gotArgs) != 2 || gotArgs[0] != "-c" || gotArgs[1] != "echo hi" {
		t.Fatalf("expected bash -c 'echo hi', got cmd=%q args=%v", gotCommand, gotArgs)
	}
	if result.ExitCode != 0 || result.Stdout != "hi\n" || result.Stderr != "oops\n" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSessionRunForwardsWorkingDirectory(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	var gotCWD string
	f.CommandFn = func(_, _ string, _ []string, cwd string, _ map[string]string) CommandScript {
		gotCWD = cwd
		return CommandScript{}
	}
	sbx := newTestSandbox(t, f, "sbx_cwd")

	_, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "ls", WorkingDirectory: "/work"})
	if err != nil {
		t.Fatal(err)
	}
	if gotCWD != "/work" {
		t.Fatalf("expected cwd /work, got %q", gotCWD)
	}
}

func TestSessionWriteBinaryFileCreatesParentDirsAndWrites(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	var mkdirCalls []string
	f.CommandFn = func(_, command string, args []string, _ string, _ map[string]string) CommandScript {
		if command == "mkdir" {
			mkdirCalls = append(mkdirCalls, strings.Join(args, " "))
		}
		return CommandScript{}
	}
	sbx := newTestSandbox(t, f, "sbx_write")
	session := NewSession(sbx)

	bytes := []byte{0, 1, 2, 255}
	if err := session.WriteBinaryFile(context.Background(), "/work/sub/file.bin", bytes); err != nil {
		t.Fatal(err)
	}
	if len(mkdirCalls) != 1 || mkdirCalls[0] != "-p /work/sub" {
		t.Fatalf("expected one 'mkdir -p /work/sub' call, got %v", mkdirCalls)
	}

	f.mu.Lock()
	got, ok := f.byName["sbx_write"].files["work/sub/file.bin"]
	f.mu.Unlock()
	if !ok {
		t.Fatal("expected file to be written to work/sub/file.bin")
	}
	if string(got) != string(bytes) {
		t.Fatalf("expected %v, got %v", bytes, got)
	}
}

func TestSessionWriteTextFileEncodesUTF8(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_text")
	session := NewSession(sbx)

	if err := session.WriteTextFile(context.Background(), providerutils.SandboxWriteTextFileOptions{Path: "/work/hello.txt", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := f.byName["sbx_text"].files["work/hello.txt"]
	f.mu.Unlock()
	if string(got) != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestSessionReadBinaryFileReturnsBytes(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_read")
	session := NewSession(sbx)
	if err := session.WriteBinaryFile(context.Background(), "/work/x", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	out, err := session.ReadBinaryFile(context.Background(), "/work/x")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string([]byte{1, 2, 3}) {
		t.Fatalf("expected [1 2 3], got %v", out)
	}
}

func TestSessionReadBinaryFileReturnsNilWhenMissing(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_missing")
	out, err := NewSession(sbx).ReadBinaryFile(context.Background(), "/missing")
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}

func TestSessionReadTextFileHonoursStartEndLine(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_lines")
	session := NewSession(sbx)
	if err := session.WriteTextFile(context.Background(), providerutils.SandboxWriteTextFileOptions{Path: "/x", Content: "a\nb\nc\nd\n"}); err != nil {
		t.Fatal(err)
	}

	start, end := 2, 3
	out, err := session.ReadTextFile(context.Background(), providerutils.SandboxReadTextFileOptions{Path: "/x", StartLine: &start, EndLine: &end})
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || *out != "b\nc" {
		t.Fatalf("expected 'b\\nc', got %v", out)
	}
}

func TestSessionReadWriteFileRoundTripThroughStreams(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_stream")
	session := NewSession(sbx)

	payload := []byte("streamed")
	if err := session.WriteFile(context.Background(), "/work/streamed.txt", strings.NewReader(string(payload))); err != nil {
		t.Fatal(err)
	}
	rc, err := session.ReadFile(context.Background(), "/work/streamed.txt")
	if err != nil {
		t.Fatal(err)
	}
	if rc == nil {
		t.Fatal("expected a non-nil reader")
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "streamed" {
		t.Fatalf("expected 'streamed', got %q", got)
	}
}

func TestSessionSpawnStreamsStdoutStderrAndResolvesWait(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.CommandFn = func(_, _ string, _ []string, _ string, _ map[string]string) CommandScript {
		return CommandScript{Stdout: "out\n", Stderr: "err\n", ExitCode: 0}
	}
	sbx := newTestSandbox(t, f, "sbx_spawn")
	session := NewSession(sbx)

	proc, err := session.Spawn(context.Background(), providerutils.SandboxProcessOptions{Command: "node x.js"})
	if err != nil {
		t.Fatal(err)
	}

	// Mirrors the TS test's `Promise.all([collect(stdout), collect(stderr),
	// wait()])`: stdout/stderr must be drained concurrently with each other
	// (and with Wait), since the underlying io.Pipe writes block until read.
	stdoutCh := make(chan []byte, 1)
	stderrCh := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(proc.Stdout()); stdoutCh <- b }()
	go func() { b, _ := io.ReadAll(proc.Stderr()); stderrCh <- b }()
	result, err := proc.Wait()
	if err != nil {
		t.Fatal(err)
	}
	stdout := <-stdoutCh
	stderr := <-stderrCh
	if string(stdout) != "out\n" {
		t.Fatalf("expected 'out\\n', got %q", stdout)
	}
	if string(stderr) != "err\n" {
		t.Fatalf("expected 'err\\n', got %q", stderr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestSessionSpawnSurfacesNonZeroExitCode(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.CommandFn = func(_, _ string, _ []string, _ string, _ map[string]string) CommandScript {
		return CommandScript{ExitCode: 7}
	}
	sbx := newTestSandbox(t, f, "sbx_exit7")
	proc, err := NewSession(sbx).Spawn(context.Background(), providerutils.SandboxProcessOptions{Command: "exit 7"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := proc.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.ExitCode)
	}
}

func TestSessionSpawnKillDelegatesToCommand(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_kill")
	proc, err := NewSession(sbx).Spawn(context.Background(), providerutils.SandboxProcessOptions{Command: "sleep 10"})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("expected kill to succeed, got %v", err)
	}
}

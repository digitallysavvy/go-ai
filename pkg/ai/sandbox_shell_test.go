package ai

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellSandboxWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, err := NewShellSandbox().RunCommand(context.Background(), SandboxRunCommandOptions{
		Command:          "cat marker.txt",
		WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("RunCommand() error = %v stderr=%s", err, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "ok" {
		t.Fatalf("stdout = %q, want ok", result.Stdout)
	}
}

func TestShellSandboxCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := NewShellSandbox().RunCommand(ctx, SandboxRunCommandOptions{Command: "sleep 1"})
	if err == nil {
		t.Fatal("RunCommand() error = nil, want cancellation")
	}
}

func TestShellSandboxSpawnReturnsBeforeProcessExitAndStreams(t *testing.T) {
	process, err := NewShellSandbox().Spawn(context.Background(), SandboxSpawnOptions{
		Command: "printf out; printf err >&2; sleep 0.2",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if process.PID() == 0 {
		t.Fatal("PID() = 0, want process id")
	}
	if _, ok := process.ExitCode(); ok {
		t.Fatal("ExitCode() ok before wait = true, want false")
	}

	stdoutCh := make(chan string, 1)
	stderrCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(process.Stdout())
		stdoutCh <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(process.Stderr())
		stderrCh <- string(data)
	}()

	resultCh := make(chan SandboxProcessResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := process.Wait()
		resultCh <- result
		errCh <- err
	}()

	select {
	case <-resultCh:
		t.Fatal("Wait returned before long-running command exited")
	case <-time.After(25 * time.Millisecond):
	}

	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if code, ok := process.ExitCode(); !ok || code != 0 {
		t.Fatalf("ExitCode() = (%d,%v), want (0,true)", code, ok)
	}
	if stdout := <-stdoutCh; stdout != "out" {
		t.Fatalf("stdout = %q, want out", stdout)
	}
	if stderr := <-stderrCh; stderr != "err" {
		t.Fatalf("stderr = %q, want err", stderr)
	}
}

func TestShellSandboxSpawnKill(t *testing.T) {
	process, err := NewShellSandbox().Spawn(context.Background(), SandboxSpawnOptions{Command: "sleep 5"})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("second Kill() error = %v", err)
	}
	result, err := process.Wait()
	if err == nil {
		t.Fatal("Wait() error = nil after kill, want process error")
	}
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = %d, want non-zero after kill", result.ExitCode)
	}
}

func TestShellSandboxSpawnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	process, err := NewShellSandbox().Spawn(ctx, SandboxSpawnOptions{Command: "sleep 5"})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	cancel()
	_, err = process.Wait()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
}

func TestShellSandboxFileHelpers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	sb := NewShellSandbox()

	if err := sb.WriteTextFile(context.Background(), SandboxWriteTextFileOptions{Path: path, Content: "one\ntwo\nthree"}); err != nil {
		t.Fatalf("WriteTextFile() error = %v", err)
	}
	data, err := sb.ReadBinaryFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadBinaryFile() error = %v", err)
	}
	if string(data) != "one\ntwo\nthree" {
		t.Fatalf("ReadBinaryFile() = %q", data)
	}
	start, end := 2, 3
	text, err := sb.ReadTextFile(context.Background(), SandboxReadTextFileOptions{Path: path, StartLine: &start, EndLine: &end})
	if err != nil {
		t.Fatalf("ReadTextFile() error = %v", err)
	}
	if text == nil || *text != "two\nthree" {
		t.Fatalf("ReadTextFile() = %v", text)
	}
	missing, err := sb.ReadTextFile(context.Background(), SandboxReadTextFileOptions{Path: filepath.Join(dir, "missing.txt")})
	if err != nil {
		t.Fatalf("ReadTextFile(missing) error = %v", err)
	}
	if missing != nil {
		t.Fatalf("ReadTextFile(missing) = %q, want nil", *missing)
	}
}

func TestShellSandboxDescription(t *testing.T) {
	sb := NewShellSandbox(WithShellSandboxDescription("Ubuntu 22.04, root: /workspace"))
	if got, want := sb.Description(), "Ubuntu 22.04, root: /workspace"; got != want {
		t.Fatalf("Description() = %q, want %q", got, want)
	}
}

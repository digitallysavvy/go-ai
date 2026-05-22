package ai

import (
	"context"
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

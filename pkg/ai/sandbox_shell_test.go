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
	result, err := NewShellSandbox().Execute(context.Background(), "cat marker.txt", SandboxExecuteOptions{
		WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v stderr=%s", err, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "ok" {
		t.Fatalf("stdout = %q, want ok", result.Stdout)
	}
}

func TestShellSandboxCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := NewShellSandbox().Execute(ctx, "sleep 1", SandboxExecuteOptions{})
	if err == nil {
		t.Fatal("Execute() error = nil, want cancellation")
	}
}

func TestShellSandboxDescription(t *testing.T) {
	sb := NewShellSandbox(WithShellSandboxDescription("Ubuntu 22.04, root: /workspace"))
	if got, want := sb.Description(), "Ubuntu 22.04, root: /workspace"; got != want {
		t.Fatalf("Description() = %q, want %q", got, want)
	}
}

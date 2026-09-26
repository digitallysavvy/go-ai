package bridge

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Tests for diagnostics.go: FormatBridgeError, LogBridgeError,
// CreateBridgeStartupError and the process stream forwarders. Mirrors the
// intent of TS bridge-diagnostics formatting and the startup-error message
// shape asserted throughout claude-code-harness.test.ts.

func TestFormatBridgeError(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"nil", nil, "null"},
		{"string", "boom", "boom"},
		{"go error", errors.New("boom"), "boom"},
		{"diagnostic error with stack", &harness.DiagnosticError{Name: "Error", Message: "boom", Stack: "Error: boom\n  at x"}, "Error: boom\n  at x"},
		{"diagnostic error without stack", &harness.DiagnosticError{Name: "Error", Message: "boom"}, "Error: boom"},
		{"diagnostic error without name or stack", &harness.DiagnosticError{Message: "boom"}, "boom"},
		{"map with message", map[string]any{"name": "Error", "message": "boom", "stack": "Error: boom"}, "Error: boom"},
		{"unrecognized value", map[string]any{"foo": "bar"}, `{"foo":"bar"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatBridgeError(tc.value); got != tc.want {
				t.Fatalf("FormatBridgeError(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestLogBridgeError(t *testing.T) {
	var lines []string
	LogBridgeError(LogBridgeErrorOptions{
		HarnessID: "claude-code",
		SessionID: "s1",
		Context:   "bridge emitted an error frame",
		Error:     "line one\nline two\n",
		Write:     func(line string) { lines = append(lines, line) },
	})
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want 2", lines)
	}
	wantPrefix := "[harness:claude-code:error session=s1] bridge emitted an error frame: "
	if !strings.HasPrefix(lines[0], wantPrefix+"line one") {
		t.Fatalf("lines[0] = %q, want prefix %q", lines[0], wantPrefix)
	}
	if !strings.Contains(lines[1], "line two") {
		t.Fatalf("lines[1] = %q", lines[1])
	}
}

func TestLineTail(t *testing.T) {
	tail := NewLineTail(3)
	for _, l := range []string{"a", "b", "c", "d"} {
		tail.Push(l)
	}
	got := tail.Lines()
	want := []string{"b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Lines() = %v, want %v", got, want)
		}
	}
}

func TestLineTailConcurrentPush(t *testing.T) {
	tail := NewLineTail(100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tail.Push("line")
			_ = n
		}(i)
	}
	wg.Wait()
	if len(tail.Lines()) != 20 {
		t.Fatalf("Lines() length = %d, want 20", len(tail.Lines()))
	}
}

// TS: startup errors carry the message, exit code and stdout/stderr tails
// ("<label> exited before becoming ready. Exit code: N.\n\nstdout:\n...\n\nstderr:\n...").
func TestCreateBridgeStartupErrorFormat(t *testing.T) {
	proc := &procWithExitCode{code: 1}
	stderrTail := NewLineTail(10)
	stderrTail.Push("stderr line 1")
	done := make(chan struct{})
	close(done)

	err := CreateBridgeStartupError(StartupErrorOptions{
		Message:    "claude-code bridge exited before becoming ready.",
		Proc:       proc,
		StdoutTail: []string{"stdout line 1"},
		StderrTail: stderrTail,
		StderrDone: done,
	})
	want := "claude-code bridge exited before becoming ready. Exit code: 1.\n\nstdout:\nstdout line 1\n\nstderr:\nstderr line 1"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestCreateBridgeStartupErrorNoExitCode(t *testing.T) {
	err := CreateBridgeStartupError(StartupErrorOptions{
		Message: "claude-code bridge did not become ready in time.",
	})
	want := "claude-code bridge did not become ready in time."
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// procWithExitCode implements providerutils.SandboxProcess with a
// pre-populated exit code, avoiding the 250ms Wait() grace window.
type procWithExitCode struct{ code int }

func (p *procWithExitCode) PID() int              { return 1 }
func (p *procWithExitCode) Stdout() io.Reader     { return nil }
func (p *procWithExitCode) Stderr() io.Reader     { return nil }
func (p *procWithExitCode) Kill() error           { return nil }
func (p *procWithExitCode) ExitCode() (int, bool) { return p.code, true }
func (p *procWithExitCode) Wait() (providerutils.SandboxProcessResult, error) {
	return providerutils.SandboxProcessResult{ExitCode: p.code}, nil
}

func TestForwardBridgeProcessStream(t *testing.T) {
	r, w := io.Pipe()
	var lines []string
	var mu sync.Mutex
	tail := NewLineTail(10)
	done := ForwardBridgeProcessStream(r, ForwardOptions{
		StreamName: "stderr",
		Source:     "claude-code",
		Tail:       tail,
		Write: func(line string) {
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		},
	})
	_, _ = w.Write([]byte("hello\nworld\n"))
	_ = w.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ForwardBridgeProcessStream did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want 2", lines)
	}
	if lines[0] != "[harness:claude-code:stderr] hello\n" {
		t.Fatalf("lines[0] = %q", lines[0])
	}
	if got := tail.Lines(); len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Fatalf("tail = %v", got)
	}
}

func TestDrainBridgeProcessStream(t *testing.T) {
	r, w := io.Pipe()
	done := DrainBridgeProcessStream(r)
	_, _ = w.Write([]byte("ignored"))
	_ = w.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DrainBridgeProcessStream did not finish")
	}
}

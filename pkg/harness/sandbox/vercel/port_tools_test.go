package vercel

import (
	"context"
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS 1d2c2ff2e4 (vercel-sandbox-session.test.ts "drains capped output to
// completion without using native output buffers").
func TestPortToolsSessionRunBoundedKeepsPrefixAndLengths(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	var gotArgs []string
	f.CommandFn = func(_, _ string, args []string, _ string, _ map[string]string) CommandScript {
		gotArgs = args
		return CommandScript{Stdout: "ab" + strings.Repeat("x", 4<<20), Stderr: "error!", ExitCode: 7}
	}
	sbx := newTestSandbox(t, f, "sbx_bounded")
	result, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "noisy; exit 7", MaxOutputLength: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := providerutils.SandboxRunResult{Stdout: "abxx", Stderr: "erro", ExitCode: 7, StdoutLength: 2 + 4<<20, StderrLength: 6}
	if result != want {
		t.Fatalf("result = %+v", result)
	}
	if len(gotArgs) != 2 || gotArgs[1] != "noisy; exit 7" {
		t.Fatalf("args = %v", gotArgs)
	}
}

func TestPortToolsSessionRunBoundedEmptyOutputAndUnicode(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.CommandFn = func(_, _ string, _ []string, _ string, _ map[string]string) CommandScript {
		return CommandScript{Stdout: "a😀b"}
	}
	sbx := newTestSandbox(t, f, "sbx_bounded_unicode")
	result, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "unicode", MaxOutputLength: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "a" || result.Stderr != "" || result.StdoutLength != 4 || result.StderrLength != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPortToolsSessionRunBoundedRejectsInvalidLimit(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_bounded_invalid")
	_, err := NewSession(sbx).Run(context.Background(), providerutils.SandboxProcessOptions{Command: "x", MaxOutputLength: -1})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) {
		t.Fatalf("err = %v", err)
	}
}

func TestPortToolsSessionRunBoundedAbortedBeforeStart(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_bounded_abort")
	reason := errors.New("cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	_, err := NewSession(sbx).Run(ctx, providerutils.SandboxProcessOptions{Command: "x", MaxOutputLength: 4})
	if !errors.Is(err, reason) {
		t.Fatalf("err = %v", err)
	}
}

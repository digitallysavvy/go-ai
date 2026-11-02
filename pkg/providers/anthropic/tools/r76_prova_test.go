package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type r76provaSession struct {
	gotCtx  context.Context
	gotOpts providerutils.SandboxProcessOptions
	result  providerutils.SandboxRunResult
	err     error
}

func (s *r76provaSession) Description() string { return "test sandbox" }
func (s *r76provaSession) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	s.gotCtx, s.gotOpts = ctx, opts
	return s.result, s.err
}
func (s *r76provaSession) Spawn(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return nil, errors.New("unused")
}
func (s *r76provaSession) ReadFile(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (s *r76provaSession) ReadBinaryFile(context.Context, string) ([]byte, error)  { return nil, nil }
func (s *r76provaSession) ReadTextFile(context.Context, providerutils.SandboxReadTextFileOptions) (*string, error) {
	return nil, nil
}
func (s *r76provaSession) WriteFile(context.Context, string, io.Reader) error { return nil }
func (s *r76provaSession) WriteBinaryFile(context.Context, string, []byte) error {
	return nil
}
func (s *r76provaSession) WriteTextFile(context.Context, providerutils.SandboxWriteTextFileOptions) error {
	return nil
}

type r76provaCtxKey struct{}

// TS bash_20250124.test.ts "passes abort signal to sandbox command
// execution": the default execute runs the command through the call's
// sandbox session (sandbox.run({command, abortSignal})) and returns its
// {exitCode, stdout, stderr} result.
func TestR76prova_BashRunsThroughSandboxSession(t *testing.T) {
	for name, tool := range map[string]types.Tool{"20250124": Bash20250124(), "20241022": Bash20241022()} {
		t.Run(name, func(t *testing.T) {
			session := &r76provaSession{result: providerutils.SandboxRunResult{ExitCode: 2, Stdout: "out", Stderr: "err"}}
			ctx := context.WithValue(t.Context(), r76provaCtxKey{}, "abort")
			out, err := tool.Execute(ctx, map[string]interface{}{"command": "ls"}, types.ToolExecutionOptions{ExperimentalSandbox: session})
			if err != nil {
				t.Fatal(err)
			}
			if session.gotOpts.Command != "ls" || session.gotCtx.Value(r76provaCtxKey{}) != "abort" {
				t.Fatalf("run got %+v", session.gotOpts)
			}
			b, _ := json.Marshal(out)
			if string(b) != `{"exitCode":2,"stdout":"out","stderr":"err"}` {
				t.Fatalf("output = %s", b)
			}

			session.err = errors.New("boom")
			if _, err := tool.Execute(ctx, map[string]interface{}{"command": "ls"}, types.ToolExecutionOptions{ExperimentalSandbox: session}); err == nil || err.Error() != "boom" {
				t.Fatalf("err = %v", err)
			}

			if _, err := tool.Execute(ctx, map[string]interface{}{"command": "ls"}, types.ToolExecutionOptions{}); err == nil || err.Error() != "Sandbox session is not available" {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

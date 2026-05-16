package ai

import (
	"bytes"
	"context"
	"os/exec"
)

// ShellSandbox executes commands through the local shell. It is the default
// concrete sandbox abstraction for callers that want TypeScript-style shell
// execution with Go context cancellation.
type ShellSandbox struct {
	Shell string
}

// NewShellSandbox returns a sandbox that executes commands with /bin/sh.
func NewShellSandbox() *ShellSandbox {
	return &ShellSandbox{Shell: "/bin/sh"}
}

// Execute runs command in the configured shell.
func (s *ShellSandbox) Execute(ctx context.Context, command string, opts SandboxExecuteOptions) (SandboxExecuteResult, error) {
	shell := s.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)
	if opts.WorkingDirectory != "" {
		cmd.Dir = opts.WorkingDirectory
	}
	if len(opts.Environment) > 0 {
		env := make([]string, 0, len(opts.Environment))
		for k, v := range opts.Environment {
			env = append(env, k+"="+v)
		}
		cmd.Env = append(cmd.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := SandboxExecuteResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	return result, err
}

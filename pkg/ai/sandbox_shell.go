package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

// ShellSandbox executes commands through the local shell. It is the default
// concrete sandbox abstraction for callers that want TypeScript-style shell
// execution with Go context cancellation.
type ShellSandbox struct {
	Shell       string
	description string
}

// NewShellSandbox returns a sandbox that executes commands with /bin/sh.
func NewShellSandbox(opts ...ShellSandboxOption) *ShellSandbox {
	sb := &ShellSandbox{Shell: "/bin/sh"}
	for _, opt := range opts {
		if opt != nil {
			opt(sb)
		}
	}
	return sb
}

// ShellSandboxOption configures ShellSandbox construction.
type ShellSandboxOption func(*ShellSandbox)

// WithShellSandboxDescription sets a sandbox description appended to model instructions.
func WithShellSandboxDescription(description string) ShellSandboxOption {
	return func(s *ShellSandbox) {
		s.description = description
	}
}

// Description returns the sandbox description for instruction injection.
func (s *ShellSandbox) Description() string {
	return s.description
}

// RunCommand runs command in the configured shell.
func (s *ShellSandbox) RunCommand(ctx context.Context, opts SandboxRunCommandOptions) (SandboxRunCommandResult, error) {
	shell := s.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", opts.Command)
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
	result := SandboxRunCommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	return result, err
}

// Execute is a deprecated compatibility wrapper for older callers.
func (s *ShellSandbox) Execute(ctx context.Context, command string, opts SandboxExecuteOptions) (SandboxExecuteResult, error) {
	opts.Command = command
	return s.RunCommand(ctx, opts)
}

// ReadFile opens a file for streaming. It returns nil when the file does not exist.
func (s *ShellSandbox) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// ReadBinaryFile reads a file as bytes. It returns nil when the file does not exist.
func (s *ShellSandbox) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// ReadTextFile reads a UTF-8 text file with optional 1-based line slicing.
func (s *ShellSandbox) ReadTextFile(ctx context.Context, opts SandboxReadTextFileOptions) (*string, error) {
	data, err := s.ReadBinaryFile(ctx, opts.Path)
	if err != nil || data == nil {
		return nil, err
	}
	text := string(data)
	if opts.StartLine != nil || opts.EndLine != nil {
		lines := strings.Split(text, "\n")
		start := 1
		if opts.StartLine != nil && *opts.StartLine > 1 {
			start = *opts.StartLine
		}
		end := len(lines)
		if opts.EndLine != nil && *opts.EndLine < end {
			end = *opts.EndLine
		}
		if start > end || start > len(lines) {
			empty := ""
			return &empty, nil
		}
		text = strings.Join(lines[start-1:end], "\n")
	}
	return &text, nil
}

// WriteFile writes stream content to a path.
func (s *ShellSandbox) WriteFile(ctx context.Context, path string, content io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	_, err = io.Copy(f, content)
	return err
}

// WriteBinaryFile writes bytes to a path.
func (s *ShellSandbox) WriteBinaryFile(ctx context.Context, path string, content []byte) error {
	return s.WriteFile(ctx, path, bytes.NewReader(content))
}

// WriteTextFile writes text to a path.
func (s *ShellSandbox) WriteTextFile(ctx context.Context, opts SandboxWriteTextFileOptions) error {
	return s.WriteFile(ctx, opts.Path, strings.NewReader(opts.Content))
}

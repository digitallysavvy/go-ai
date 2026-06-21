package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
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

// Run runs command in the configured shell.
func (s *ShellSandbox) Run(ctx context.Context, opts SandboxProcessOptions) (SandboxRunResult, error) {
	process, err := s.spawn(ctx, opts.Command, opts.WorkingDirectory, opts.Env)
	if err != nil {
		return SandboxRunResult{}, err
	}
	var stdout, stderr bytes.Buffer
	var copyWG sync.WaitGroup
	copyWG.Add(2)
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(&stdout, process.Stdout())
	}()
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(&stderr, process.Stderr())
	}()
	waitResult, waitErr := process.Wait()
	copyWG.Wait()
	return SandboxRunResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: waitResult.ExitCode,
	}, waitErr
}

// Spawn starts command in the configured shell and returns immediately with a process handle.
func (s *ShellSandbox) Spawn(ctx context.Context, opts SandboxProcessOptions) (SandboxProcess, error) {
	return s.spawn(ctx, opts.Command, opts.WorkingDirectory, opts.Env)
}

func (s *ShellSandbox) spawn(ctx context.Context, command, workingDirectory string, environment map[string]string) (SandboxProcess, error) {
	shell := s.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)
	if workingDirectory != "" {
		cmd.Dir = workingDirectory
	}
	if len(environment) > 0 {
		env := make([]string, 0, len(environment))
		for k, v := range environment {
			env = append(env, k+"="+v)
		}
		cmd.Env = append(cmd.Environ(), env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return newShellSandboxProcess(ctx, cmd, stdout, stderr), nil
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
	text, err := decodeSandboxText(data, opts.Encoding)
	if err != nil {
		return nil, err
	}
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
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
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
	data, err := encodeSandboxText(opts.Content, opts.Encoding)
	if err != nil {
		return err
	}
	return s.WriteFile(ctx, opts.Path, bytes.NewReader(data))
}

func decodeSandboxText(data []byte, encodingLabel string) (string, error) {
	if encodingLabel == "" || strings.EqualFold(encodingLabel, "utf-8") || strings.EqualFold(encodingLabel, "utf8") {
		return string(data), nil
	}
	enc, err := htmlindex.Get(encodingLabel)
	if err != nil {
		return "", err
	}
	decoded, _, err := transform.Bytes(enc.NewDecoder(), data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func encodeSandboxText(text string, encodingLabel string) ([]byte, error) {
	if encodingLabel == "" || strings.EqualFold(encodingLabel, "utf-8") || strings.EqualFold(encodingLabel, "utf8") {
		return []byte(text), nil
	}
	enc, err := htmlindex.Get(encodingLabel)
	if err != nil {
		return nil, err
	}
	encoded, _, err := transform.Bytes(enc.NewEncoder(), []byte(text))
	return encoded, err
}

type shellSandboxProcess struct {
	ctx    context.Context
	cmd    *exec.Cmd
	stdout io.Reader
	stderr io.Reader

	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
	exitCode int
	exited   bool

	killOnce sync.Once
	killErr  error
	mu       sync.RWMutex
}

func newShellSandboxProcess(ctx context.Context, cmd *exec.Cmd, stdout, stderr io.Reader) *shellSandboxProcess {
	return &shellSandboxProcess{
		ctx:      ctx,
		cmd:      cmd,
		stdout:   stdout,
		stderr:   stderr,
		waitDone: make(chan struct{}),
		exitCode: -1,
	}
}

func (p *shellSandboxProcess) PID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *shellSandboxProcess) Stdout() io.Reader {
	return p.stdout
}

func (p *shellSandboxProcess) Stderr() io.Reader {
	return p.stderr
}

func (p *shellSandboxProcess) Wait() (SandboxProcessResult, error) {
	p.waitOnce.Do(func() {
		err := p.cmd.Wait()
		exitCode := -1
		if p.cmd.ProcessState != nil {
			exitCode = p.cmd.ProcessState.ExitCode()
		}
		if ctxErr := p.ctx.Err(); ctxErr != nil && exitCode != 0 {
			err = ctxErr
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				err = nil
			}
		}
		p.mu.Lock()
		p.exitCode = exitCode
		p.exited = true
		p.waitErr = err
		p.mu.Unlock()
		close(p.waitDone)
	})
	<-p.waitDone

	p.mu.RLock()
	result := SandboxProcessResult{ExitCode: p.exitCode}
	err := p.waitErr
	p.mu.RUnlock()
	return result, err
}

func (p *shellSandboxProcess) Kill() error {
	p.killOnce.Do(func() {
		if p.cmd == nil || p.cmd.Process == nil {
			return
		}
		p.mu.RLock()
		exited := p.exited
		p.mu.RUnlock()
		if exited {
			return
		}
		err := p.cmd.Process.Kill()
		if errors.Is(err, os.ErrProcessDone) {
			err = nil
		}
		p.killErr = err
	})
	return p.killErr
}

func (p *shellSandboxProcess) ExitCode() (int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitCode, p.exited
}

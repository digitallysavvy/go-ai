package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

// shellSandboxPipeGrace is how long Run waits, after its ctx is done, for the
// command's output pipes to reach EOF before closing them.
const shellSandboxPipeGrace = 500 * time.Millisecond

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
	// Drain both pipes to EOF (which the copy goroutines see once the child
	// closes them at process exit) before calling Wait. exec.Cmd.Wait closes
	// the StdoutPipe/StderrPipe read ends once it observes the process exit;
	// calling it concurrently with in-flight reads races the pipe close
	// against io.Copy and can truncate output under load (see
	// https://pkg.go.dev/os/exec#Cmd.StdoutPipe: "it is incorrect to call
	// Wait before all reads from the pipe have completed").
	//
	// A cancelled ctx kills only the shell; a child it forked (sh -c "a; b")
	// keeps the pipes open, which would hold this call until that child
	// exits. After ctx is done the pipes are closed following a short grace
	// period, so Run returns promptly with whatever output was captured.
	copied := make(chan struct{})
	go func() {
		select {
		case <-copied:
			return
		case <-ctx.Done():
		}
		select {
		case <-copied:
			return
		case <-time.After(shellSandboxPipeGrace):
		}
		for _, r := range []io.Reader{process.Stdout(), process.Stderr()} {
			if c, ok := r.(io.Closer); ok {
				_ = c.Close()
			}
		}
	}()
	copyWG.Wait()
	close(copied)
	waitResult, waitErr := process.Wait()
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
	text = providerutils.ExtractLines(text, opts.StartLine, opts.EndLine)
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
	_, err = io.Copy(f, content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
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

// The optional SandboxSession operations TS sandbox sessions implement
// (just-bash stat / listDirectory / deleteFile). Paths are used as given, as
// ReadFile and WriteFile do.
var (
	_ providerutils.SandboxStatter         = (*ShellSandbox)(nil)
	_ providerutils.SandboxDirectoryLister = (*ShellSandbox)(nil)
	_ providerutils.SandboxFileDeleter     = (*ShellSandbox)(nil)
)

// Stat implements providerutils.SandboxStatter (TS SandboxSession.stat):
// symbolic links are followed; a missing path (or one under a file) is
// (nil, nil).
func (s *ShellSandbox) Stat(ctx context.Context, path string) (*providerutils.SandboxFileStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if shellSandboxMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	typ := providerutils.SandboxPathTypeOther
	switch {
	case info.Mode().IsRegular():
		typ = providerutils.SandboxPathTypeFile
	case info.IsDir():
		typ = providerutils.SandboxPathTypeDirectory
	}
	return &providerutils.SandboxFileStat{Type: typ, Size: info.Size()}, nil
}

// ListDirectory implements providerutils.SandboxDirectoryLister (TS
// SandboxSession.listDirectory): entries are not followed, so a symbolic link
// is reported as "symlink"; a missing directory is (nil, nil).
func (s *ShellSandbox) ListDirectory(ctx context.Context, path string) ([]providerutils.SandboxDirectoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if shellSandboxMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Failed to list %s: Not a directory", path) //nolint:staticcheck // TS error text
	}
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]providerutils.SandboxDirectoryEntry, 0, len(dirEntries))
	for _, e := range dirEntries {
		typ := providerutils.SandboxPathTypeOther
		switch mode := e.Type(); {
		case mode&os.ModeSymlink != 0:
			typ = providerutils.SandboxPathTypeSymlink
		case mode.IsRegular():
			typ = providerutils.SandboxPathTypeFile
		case mode.IsDir():
			typ = providerutils.SandboxPathTypeDirectory
		}
		entries = append(entries, providerutils.SandboxDirectoryEntry{Name: e.Name(), Type: typ})
	}
	return entries, nil
}

// DeleteFile implements providerutils.SandboxFileDeleter (TS
// SandboxSession.deleteFile): a symbolic link is deleted itself, a missing
// file is false, a directory is an error.
func (s *ShellSandbox) DeleteFile(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if shellSandboxMissing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("Cannot delete %s: it is a directory.", path) //nolint:staticcheck // TS error text
	}
	if err := os.Remove(path); err != nil {
		if shellSandboxMissing(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// shellSandboxMissing reports a path that does not exist, including one under
// a regular file (ENOTDIR), which TS treats as missing too.
func shellSandboxMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func decodeSandboxText(data []byte, encodingLabel string) (string, error) {
	return providerutils.DecodeSandboxText(data, encodingLabel)
}

func encodeSandboxText(text string, encodingLabel string) ([]byte, error) {
	return providerutils.EncodeSandboxText(text, encodingLabel)
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

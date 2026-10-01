package vercel

import (
	"context"
	"io"
	"path"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// posixDir returns the directory portion of a posix path (mirrors Node's
// `path.posix.dirname`, used by TS `writeBinaryFile` to mkdir -p the parent).
func posixDir(p string) string { return path.Dir(p) }

// Session is a providerutils.SandboxSession backed by a Vercel Sandbox. It is
// the tool-safe surface (file I/O, exec, spawn) — what
// NetworkSession.Restricted() returns. Ports
// packages/sandbox-vercel/src/vercel-sandbox-session.ts
// (VercelSandboxSession).
type Session struct {
	sandbox *Sandbox
}

var _ providerutils.SandboxSession = (*Session)(nil)

// NewSession wraps an existing native sandbox as a restricted session.
func NewSession(sandbox *Sandbox) *Session { return &Session{sandbox: sandbox} }

// Description mentions the sandbox name (mirrors TS `description`).
func (s *Session) Description() string {
	return "Vercel Sandbox (name: " + s.sandbox.Name() + ").\n" +
		"Filesystem changes persist for the lifetime of the sandbox."
}

// Run runs opts.Command via `bash -c`, blocking until it exits.
//
// On error, the returned result still carries whatever stdout/stderr
// RunCommand had already accumulated (mirrors the local sandbox's Session.Run,
// which likewise returns accumulated output alongside a non-nil error rather
// than a zero value) — see Sandbox.RunCommand / APIClient.RunCommandWait.
func (s *Session) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	result, err := s.sandbox.RunCommand(ctx, "bash", []string{"-c", opts.Command}, opts.WorkingDirectory, opts.Env)
	return providerutils.SandboxRunResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode}, err
}

// Spawn starts opts.Command via `bash -c`, detached, streaming output live.
func (s *Session) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	proc, err := s.sandbox.SpawnCommand(ctx, "bash", []string{"-c", opts.Command}, opts.WorkingDirectory, opts.Env)
	if err != nil {
		return nil, err
	}
	return &sandboxProcessAdapter{ctx: ctx, proc: proc}, nil
}

// sandboxProcessAdapter adapts *RemoteProcess (whose Wait/Kill take a
// context) to the context-free providerutils.SandboxProcess interface, using
// the context Spawn was called with — mirroring the TS closure that captures
// `abortSignal` at spawn time.
type sandboxProcessAdapter struct {
	ctx  context.Context
	proc *RemoteProcess
}

func (a *sandboxProcessAdapter) PID() int              { return a.proc.PID() }
func (a *sandboxProcessAdapter) Stdout() io.Reader     { return a.proc.Stdout() }
func (a *sandboxProcessAdapter) Stderr() io.Reader     { return a.proc.Stderr() }
func (a *sandboxProcessAdapter) ExitCode() (int, bool) { return a.proc.ExitCode() }

func (a *sandboxProcessAdapter) Wait() (providerutils.SandboxProcessResult, error) {
	code, err := a.proc.Wait(a.ctx)
	if err != nil {
		return providerutils.SandboxProcessResult{}, err
	}
	return providerutils.SandboxProcessResult{ExitCode: code}, nil
}

func (a *sandboxProcessAdapter) Kill() error {
	return a.proc.Kill(a.ctx)
}

// ReadFile reads path, returning nil when it does not exist.
func (s *Session) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	data, err := s.ReadBinaryFile(ctx, path)
	if err != nil || data == nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

// ReadBinaryFile reads path fully, returning nil when it does not exist.
func (s *Session) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	return s.sandbox.ReadFileToBuffer(ctx, path, "")
}

// ReadTextFile reads a UTF-8 file with optional 1-based line slicing.
func (s *Session) ReadTextFile(ctx context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	data, err := s.ReadBinaryFile(ctx, opts.Path)
	if err != nil || data == nil {
		return nil, err
	}
	text := string(data)
	if opts.StartLine != nil || opts.EndLine != nil {
		lines := strings.Split(text, "\n")
		start, end := 1, len(lines)
		if opts.StartLine != nil && *opts.StartLine > 1 {
			start = *opts.StartLine
		}
		if opts.EndLine != nil && *opts.EndLine < end {
			end = *opts.EndLine
		}
		if start > end {
			text = ""
		} else {
			text = strings.Join(lines[start-1:end], "\n")
		}
	}
	return &text, nil
}

// WriteFile writes content, creating parent directories first (matching TS,
// which mkdir -p's the parent before every write).
func (s *Session) WriteFile(ctx context.Context, path string, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	return s.WriteBinaryFile(ctx, path, data)
}

// WriteBinaryFile mkdir -p's the parent directory, then writes bytes to
// path. Mirrors TS `VercelSandboxSession.writeBinaryFile`.
func (s *Session) WriteBinaryFile(ctx context.Context, path string, content []byte) error {
	if parent := posixDir(path); parent != "" && parent != "." && parent != "/" {
		if _, err := s.sandbox.RunCommand(ctx, "mkdir", []string{"-p", parent}, "", nil); err != nil {
			return err
		}
	}
	return s.sandbox.WriteFiles(ctx, []WriteFileInput{{Path: path, Content: content}}, s.sandbox.CWD())
}

// WriteTextFile writes UTF-8 text to a path.
func (s *Session) WriteTextFile(ctx context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	return s.WriteBinaryFile(ctx, opts.Path, []byte(opts.Content))
}

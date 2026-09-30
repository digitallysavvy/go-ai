package vercel

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"sync"
)

// RunResult is the outcome of a blocking Sandbox.RunCommand call.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCommand runs cmd to completion, mirroring TS `Session.runCommand`'s
// non-detached (wait=true, logs=true) path: stdout/stderr are accumulated
// client-side from the interleaved log stream.
func (s *Sandbox) RunCommand(ctx context.Context, cmd string, args []string, cwd string, env map[string]string) (RunResult, error) {
	result, err := s.client.RunCommandWait(ctx, s.sessionID, runCommandRequest{
		Command: cmd,
		Args:    args,
		CWD:     cwd,
		Env:     env,
	})
	if err != nil {
		return RunResult{}, err
	}
	exitCode := 0
	if result.Command.ExitCode != nil {
		exitCode = *result.Command.ExitCode
	}
	return RunResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: exitCode}, nil
}

// RemoteProcess is a detached command started with SpawnCommand. It streams
// stdout/stderr live via GetLogs and supports Wait/Kill, mirroring the
// `Command`/`createSandboxProcess` pair in
// packages/sandbox-vercel/src/vercel-sandbox-session.ts.
type RemoteProcess struct {
	client    *APIClient
	sessionID string
	cmdID     string

	stdout, stderr *streamBuffer

	logsDone chan struct{}
	logsErr  error

	mu       sync.Mutex
	exitCode int
	exited   bool
}

// SpawnCommand starts cmd detached, mirroring TS `VercelSandboxSession.spawn`
// (`sandbox.runCommand({..., detached: true})` followed by independent
// `command.logs()` consumption).
func (s *Sandbox) SpawnCommand(ctx context.Context, cmd string, args []string, cwd string, env map[string]string) (*RemoteProcess, error) {
	created, err := s.client.RunCommandDetached(ctx, s.sessionID, runCommandRequest{
		Command: cmd, Args: args, CWD: cwd, Env: env,
	})
	if err != nil {
		return nil, err
	}
	p := &RemoteProcess{
		client: s.client, sessionID: s.sessionID, cmdID: created.ID,
		stdout: newStreamBuffer(), stderr: newStreamBuffer(),
		logsDone: make(chan struct{}), exitCode: -1,
	}
	go p.consumeLogs(ctx)
	return p, nil
}

func (p *RemoteProcess) consumeLogs(ctx context.Context) {
	err := p.client.GetLogs(ctx, p.sessionID, p.cmdID, func(entry LogEntry) {
		switch entry.Stream {
		case "stdout":
			_, _ = p.stdout.Write([]byte(entry.Data))
		case "stderr":
			_, _ = p.stderr.Write([]byte(entry.Data))
		}
	})
	p.logsErr = err
	p.stdout.Close(err)
	p.stderr.Close(err)
	close(p.logsDone)
}

// PID is unavailable via the Vercel Sandbox HTTP API.
func (p *RemoteProcess) PID() int { return 0 }

// Stdout streams the process's standard output. Reading it does not block
// stderr delivery or vice versa (see streamBuffer).
func (p *RemoteProcess) Stdout() io.Reader { return p.stdout }

// Stderr streams the process's standard error.
func (p *RemoteProcess) Stderr() io.Reader { return p.stderr }

// Wait blocks until the command exits (GET .../cmd/{id}?wait=true), then
// waits for the log stream to drain so all output has been delivered before
// returning.
func (p *RemoteProcess) Wait(ctx context.Context) (int, error) {
	command, err := p.client.GetCommand(ctx, p.sessionID, p.cmdID, true)
	if err != nil {
		return 0, err
	}
	<-p.logsDone
	if ctxErr := ctx.Err(); ctxErr != nil {
		return 0, ctxErr
	}
	exitCode := 0
	if command.ExitCode != nil {
		exitCode = *command.ExitCode
	}
	p.mu.Lock()
	p.exitCode, p.exited = exitCode, true
	p.mu.Unlock()
	return exitCode, nil
}

// Kill sends SIGTERM to the running command.
func (p *RemoteProcess) Kill(ctx context.Context) error {
	return p.client.KillCommand(ctx, p.sessionID, p.cmdID, "SIGTERM")
}

// ExitCode returns the exit code once Wait has completed.
func (p *RemoteProcess) ExitCode() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode, p.exited
}

// ReadFileToBuffer reads path (resolved against cwd when relative) fully
// into memory. Returns nil, nil when the file does not exist.
func (s *Sandbox) ReadFileToBuffer(ctx context.Context, filePath, cwd string) ([]byte, error) {
	rc, err := s.client.ReadFile(ctx, s.sessionID, filePath, cwd)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		return nil, nil
	}
	defer rc.Close() //nolint:errcheck
	return io.ReadAll(rc)
}

// WriteFileInput is one file to write via WriteFiles.
type WriteFileInput struct {
	Path    string
	Content []byte
	Mode    int64
}

// WriteFiles gzip+tars files and writes them, defaulting to the sandbox root
// as the extraction directory (mirrors TS `Session.writeFiles`, which always
// passes `extractDir: '/'`).
func (s *Sandbox) WriteFiles(ctx context.Context, files []WriteFileInput, cwd string) error {
	toWrite := make([]FileToWrite, len(files))
	for i, f := range files {
		name, err := normalizePath(f.Path, cwd, "/")
		if err != nil {
			return err
		}
		toWrite[i] = FileToWrite{Name: name, Content: f.Content, Mode: f.Mode}
	}
	return s.client.WriteFiles(ctx, s.sessionID, "/", toWrite)
}

// normalizePath ports TS `normalizePath`: relative paths resolve against cwd;
// absolute paths are normalized as-is; the result is always relative to
// extractDir.
func normalizePath(filePath, cwd, extractDir string) (string, error) {
	if !path.IsAbs(cwd) {
		return "", errors.New("cwd dir must be absolute")
	}
	if !path.IsAbs(extractDir) {
		return "", errors.New("extractDir must be absolute")
	}
	var basePath string
	if path.IsAbs(filePath) {
		basePath = path.Clean(filePath)
	} else {
		basePath = path.Join(cwd, filePath)
	}
	rel, err := relPosix(extractDir, basePath)
	if err != nil {
		return "", err
	}
	return rel, nil
}

// relPosix is a minimal posix path.Rel (Go's path/filepath.Rel is OS-specific
// and rejects slash-separated input on Windows); extractDir is always "/" for
// every call site, which makes this exact: the relative path from "/" to any
// absolute path is just that path without its leading slash.
func relPosix(base, target string) (string, error) {
	if base != "/" {
		return "", errors.New("relPosix: only base \"/\" is supported")
	}
	return strings.TrimPrefix(target, "/"), nil
}

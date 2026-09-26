// Package bridgetest provides test doubles for the harness bridge transport:
// a fake WebSocket bridge server that speaks enough of the harness-v1 wire
// protocol (token auth, bridge-hello, seq/replay, user-message, stop) to
// exercise Channel and Launch without the TypeScript runtime, plus a
// controllable fake SandboxSession/SandboxProcess.
package bridgetest

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// memStream is an unbounded, single-reader byte stream: Write never blocks
// (unlike io.Pipe, whose Write blocks until a Read consumes it — deadlocking
// a test that writes fixture output before the reader goroutine starts).
// Read blocks until data is available or the stream is closed and drained.
type memStream struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newMemStream() *memStream {
	s := &memStream{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *memStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	s.mu.Unlock()
	s.cond.Broadcast()
	return len(p), nil
}

func (s *memStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) == 0 && !s.closed {
		s.cond.Wait()
	}
	if len(s.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *memStream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cond.Broadcast()
	return nil
}

// Process is a controllable fake providerutils.SandboxProcess. Tests drive it
// explicitly by writing to Stdout/Stderr and calling Exit/Kill.
type Process struct {
	stdout *memStream
	stderr *memStream

	mu       sync.Mutex
	killed   bool
	exitCode *int
	once     sync.Once
	doneCh   chan struct{}
	result   providerutils.SandboxProcessResult
}

// NewProcess returns a fresh fake process with open stdout/stderr streams.
func NewProcess() *Process {
	return &Process{stdout: newMemStream(), stderr: newMemStream(), doneCh: make(chan struct{})}
}

// PID returns a fixed fake process id.
func (p *Process) PID() int { return 4242 }

// Stdout returns the fake stdout stream.
func (p *Process) Stdout() io.Reader { return p.stdout }

// Stderr returns the fake stderr stream.
func (p *Process) Stderr() io.Reader { return p.stderr }

// WriteStdout writes s to stdout verbatim (include a trailing "\n" for a
// complete line). Never blocks, so it is safe to call before a reader starts.
func (p *Process) WriteStdout(s string) { _, _ = p.stdout.Write([]byte(s)) }

// WriteStderr writes s to stderr verbatim. Never blocks.
func (p *Process) WriteStderr(s string) { _, _ = p.stderr.Write([]byte(s)) }

// CloseStdout ends the stdout stream (EOF) without marking the process
// exited, mirroring a redirected fd closing independently of the process.
func (p *Process) CloseStdout() { _ = p.stdout.Close() }

// CloseStderr ends the stderr stream (EOF).
func (p *Process) CloseStderr() { _ = p.stderr.Close() }

// Exit marks the process exited with code, closing both output streams. Safe
// to call more than once; only the first call has effect.
func (p *Process) Exit(code int) {
	p.once.Do(func() {
		p.mu.Lock()
		c := code
		p.exitCode = &c
		p.mu.Unlock()
		_ = p.stdout.Close()
		_ = p.stderr.Close()
		p.result = providerutils.SandboxProcessResult{ExitCode: code}
		close(p.doneCh)
	})
}

// Kill terminates the fake process (exit code 0) and records that Kill was
// called.
func (p *Process) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	p.Exit(0)
	return nil
}

// Killed reports whether Kill has been called.
func (p *Process) Killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// Wait blocks until Exit/Kill is called.
func (p *Process) Wait() (providerutils.SandboxProcessResult, error) {
	<-p.doneCh
	return p.result, nil
}

// ExitCode returns the exit code once known.
func (p *Process) ExitCode() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exitCode == nil {
		return 0, false
	}
	return *p.exitCode, true
}

// Sandbox is a fake providerutils.SandboxSession backed by an in-memory file
// map and a test-supplied Spawn function.
type Sandbox struct {
	mu    sync.Mutex
	files map[string]string
	spawn func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error)
}

// NewSandbox returns an empty fake sandbox. Configure spawning with SetSpawn.
func NewSandbox() *Sandbox { return &Sandbox{files: map[string]string{}} }

// SetSpawn installs the function Spawn delegates to.
func (s *Sandbox) SetSpawn(fn func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawn = fn
}

// SetFile seeds a file (e.g. bridge-meta.json) as if written by the bridge.
func (s *Sandbox) SetFile(path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = content
}

// File returns a previously written file's content.
func (s *Sandbox) File(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.files[path]
	return v, ok
}

// Description implements providerutils.SandboxSession.
func (s *Sandbox) Description() string { return "bridgetest sandbox" }

// Run implements providerutils.SandboxSession (a no-op success).
func (s *Sandbox) Run(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	return providerutils.SandboxRunResult{}, nil
}

// Spawn implements providerutils.SandboxSession by delegating to the
// test-supplied function.
func (s *Sandbox) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	s.mu.Lock()
	fn := s.spawn
	s.mu.Unlock()
	if fn == nil {
		return nil, errors.New("bridgetest: Sandbox.Spawn is not configured")
	}
	return fn(ctx, opts)
}

// ReadFile implements providerutils.SandboxSession (unsupported).
func (s *Sandbox) ReadFile(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("bridgetest: ReadFile not supported")
}

// ReadBinaryFile implements providerutils.SandboxSession (unsupported).
func (s *Sandbox) ReadBinaryFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("bridgetest: ReadBinaryFile not supported")
}

// ReadTextFile implements providerutils.SandboxSession over the in-memory
// file map, returning nil (not an error) when the file is absent.
func (s *Sandbox) ReadTextFile(_ context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.files[opts.Path]; ok {
		return &v, nil
	}
	return nil, nil
}

// WriteFile implements providerutils.SandboxSession (unsupported).
func (s *Sandbox) WriteFile(context.Context, string, io.Reader) error {
	return errors.New("bridgetest: WriteFile not supported")
}

// WriteBinaryFile implements providerutils.SandboxSession (unsupported).
func (s *Sandbox) WriteBinaryFile(context.Context, string, []byte) error {
	return errors.New("bridgetest: WriteBinaryFile not supported")
}

// WriteTextFile implements providerutils.SandboxSession over the in-memory
// file map.
func (s *Sandbox) WriteTextFile(_ context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[opts.Path] = opts.Content
	return nil
}

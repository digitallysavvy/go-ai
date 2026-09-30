// Package local is a Go-only harness sandbox provider that runs commands
// directly on the host, inside a per-session directory, with localhost ports.
//
// It has no TypeScript counterpart (TS ships `sandbox-vercel` and the
// in-memory `sandbox-just-bash`). It exists so Go harnesses can be run and
// tested without a remote sandbox: bridges bind 0.0.0.0/127.0.0.1 on the host
// and GetPortEndpoint returns `ws://127.0.0.1:<port>`.
//
// It provides NO isolation: commands run as the current user with full host
// network and filesystem access. Use it for development and tests only.
//
// Layout per session (under Options.RootDir):
//
//	<root>/<encoded session id>/work   default working directory
//	<root>/<encoded session id>/home   HOME (unless Options.HomeDir is set)
package local

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ProviderID is the provider identifier.
const ProviderID = "local"

// Options configures the local sandbox provider.
type Options struct {
	// RootDir holds one directory per session. Defaults to
	// <os.TempDir()>/ai-sdk-harness-local.
	RootDir string

	// HomeDir, when set, is used as HOME for every session (for example the
	// real user home to reuse caches and native subscriptions). When empty,
	// each session gets an isolated HOME under its session directory.
	HomeDir string

	// Shell runs commands. Defaults to /bin/sh.
	Shell string

	// Env is added to every command's environment (after the host
	// environment and HOME, before per-command env).
	Env map[string]string

	// Ports lists the ports reported by Ports(); all localhost ports are
	// reachable regardless.
	Ports []int

	// Host used in port endpoints. Defaults to 127.0.0.1.
	Host string

	// KeepFiles keeps the session directory on Destroy.
	KeepFiles bool
}

// Provider implements harness.SandboxProvider and harness.SandboxSessionResumer.
type Provider struct {
	opts Options
}

var (
	_ harness.SandboxProvider       = (*Provider)(nil)
	_ harness.SandboxSessionResumer = (*Provider)(nil)
	_ harness.NetworkSandboxSession = (*Session)(nil)
	_ harness.PortsSetter           = (*Session)(nil)
)

// NewProvider returns a local sandbox provider.
func NewProvider(opts Options) *Provider {
	if opts.RootDir == "" {
		opts.RootDir = filepath.Join(os.TempDir(), "ai-sdk-harness-local")
	}
	if opts.Shell == "" {
		opts.Shell = "/bin/sh"
	}
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	return &Provider{opts: opts}
}

// SpecificationVersion returns "harness-sandbox-v1".
func (p *Provider) SpecificationVersion() string { return harness.SandboxSpecificationVersion }

// ProviderID returns "local".
func (p *Provider) ProviderID() string { return ProviderID }

// CreateSession creates (or reuses, for an existing SessionID) a session
// directory. OnFirstCreate runs only when the directory is freshly created.
// Identity is not used: the bootstrap marker under HOME makes re-applying a
// recipe a no-op.
func (p *Provider) CreateSession(ctx context.Context, opts harness.CreateSandboxSessionOptions) (harness.NetworkSandboxSession, error) {
	id := opts.SessionID
	if id == "" {
		id = "local-" + randomHex(8)
	}
	session, fresh, err := p.open(id, true)
	if err != nil {
		return nil, err
	}
	if fresh && opts.OnFirstCreate != nil {
		if err := opts.OnFirstCreate(ctx, session); err != nil {
			_ = session.Destroy(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	return session, nil
}

// ResumeSession reattaches to an existing session directory.
func (p *Provider) ResumeSession(_ context.Context, sessionID string) (harness.NetworkSandboxSession, error) {
	session, _, err := p.open(sessionID, false)
	return session, err
}

func (p *Provider) open(id string, create bool) (*Session, bool, error) {
	dir := filepath.Join(p.opts.RootDir, harness.EncodePathSegment(id))
	workDir := filepath.Join(dir, "work")
	homeDir := p.opts.HomeDir
	if homeDir == "" {
		homeDir = filepath.Join(dir, "home")
	}
	_, statErr := os.Stat(workDir)
	fresh := errors.Is(statErr, os.ErrNotExist)
	if fresh && !create {
		return nil, false, fmt.Errorf("local sandbox %q does not exist", id)
	}
	if statErr != nil && !fresh {
		return nil, false, statErr
	}
	for _, d := range []string{workDir, homeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, false, err
		}
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil, false, err
	}
	absHome, err := filepath.Abs(homeDir)
	if err != nil {
		return nil, false, err
	}
	return &Session{
		id:        id,
		dir:       dir,
		workDir:   filepath.ToSlash(absWork),
		homeDir:   filepath.ToSlash(absHome),
		shell:     p.opts.Shell,
		env:       p.opts.Env,
		host:      p.opts.Host,
		ports:     append([]int(nil), p.opts.Ports...),
		keepFiles: p.opts.KeepFiles,
		procs:     map[*process]struct{}{},
	}, fresh, nil
}

// Session is a local network sandbox session.
type Session struct {
	id        string
	dir       string
	workDir   string
	homeDir   string
	shell     string
	env       map[string]string
	host      string
	keepFiles bool

	mu      sync.Mutex
	ports   []int
	procs   map[*process]struct{}
	stopped bool
}

// ID returns the session id.
func (s *Session) ID() string { return s.id }

// HomeDir returns the HOME used for commands.
func (s *Session) HomeDir() string { return s.homeDir }

// Description is empty: the local sandbox adds no model instructions.
func (s *Session) Description() string { return "" }

// DefaultWorkingDirectory returns the session work directory.
func (s *Session) DefaultWorkingDirectory() string { return s.workDir }

// Ports returns the configured ports.
func (s *Session) Ports() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.ports...)
}

// SetPorts replaces the reported ports.
func (s *Session) SetPorts(_ context.Context, ports []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ports = append([]int(nil), ports...)
	sort.Ints(s.ports)
	return nil
}

// GetPortEndpoint returns `<protocol>://<host>:<port>` (default http).
func (s *Session) GetPortEndpoint(_ context.Context, opts harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	if opts.Port <= 0 || opts.Port > 65535 {
		return harness.PortEndpoint{}, fmt.Errorf("local sandbox: invalid port %d", opts.Port)
	}
	protocol := opts.Protocol
	if protocol == "" {
		protocol = harness.PortProtocolHTTP
	}
	return harness.PortEndpoint{URL: fmt.Sprintf("%s://%s:%d", protocol, s.host, opts.Port)}, nil
}

// GetPortURL returns the endpoint URL.
//
// Deprecated: use GetPortEndpoint.
func (s *Session) GetPortURL(ctx context.Context, opts harness.PortEndpointOptions) (string, error) {
	endpoint, err := s.GetPortEndpoint(ctx, opts)
	return endpoint.URL, err
}

// Stop kills every process spawned by this session (whole process groups).
// Idempotent.
func (s *Session) Stop(context.Context) error {
	s.mu.Lock()
	s.stopped = true
	procs := make([]*process, 0, len(s.procs))
	for p := range s.procs {
		procs = append(procs, p)
	}
	s.mu.Unlock()
	var errs []error
	for _, p := range procs {
		if err := p.Kill(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Destroy stops the session and removes its directory (unless KeepFiles).
func (s *Session) Destroy(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	if s.keepFiles {
		return nil
	}
	return os.RemoveAll(s.dir)
}

// Restricted returns a filesystem/process-only view of the same sandbox.
func (s *Session) Restricted() providerutils.SandboxSession { return restricted{s} }

func (s *Session) resolve(p string) string {
	if p == "" {
		return s.workDir
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.workDir, p)
}

func (s *Session) environment(extra map[string]string) []string {
	env := os.Environ()
	env = append(env, "HOME="+s.homeDir)
	for k, v := range s.env {
		env = append(env, k+"="+v)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// Spawn starts a command in its own process group.
func (s *Session) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return nil, errors.New("local sandbox: session is stopped")
	}
	cmd := exec.Command(s.shell, "-c", opts.Command)
	cmd.Dir = s.resolve(opts.WorkingDirectory)
	cmd.Env = s.environment(opts.Env)
	setProcessGroup(cmd)
	// os.Pipe (not StdoutPipe): Wait runs concurrently with readers and must
	// not close the read ends before all output is consumed.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = stdoutW, stderrW
	startErr := cmd.Start()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	if startErr != nil {
		_ = stdoutR.Close()
		_ = stderrR.Close()
		return nil, startErr
	}
	stdout, stderr := io.Reader(stdoutR), io.Reader(stderrR)
	p := &process{cmd: cmd, stdout: stdout, stderr: stderr, done: make(chan struct{}), exitCode: -1}
	s.mu.Lock()
	s.procs[p] = struct{}{}
	s.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Kill()
		case <-p.done:
		}
	}()
	go func() {
		<-p.done
		s.mu.Lock()
		delete(s.procs, p)
		s.mu.Unlock()
	}()
	go p.wait(ctx)
	return p, nil
}

// Run runs a command to completion and collects its output.
func (s *Session) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	proc, err := s.Spawn(ctx, opts)
	if err != nil {
		return providerutils.SandboxRunResult{}, err
	}
	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&stdout, proc.Stdout()) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&stderr, proc.Stderr()) }()
	wg.Wait()
	result, err := proc.Wait()
	return providerutils.SandboxRunResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: result.ExitCode}, err
}

// ReadFile opens a file; nil when it does not exist.
func (s *Session) ReadFile(_ context.Context, path string) (io.ReadCloser, error) {
	f, err := os.Open(s.resolve(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// ReadBinaryFile reads a file; nil when it does not exist.
func (s *Session) ReadBinaryFile(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(s.resolve(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// ReadTextFile reads a UTF-8 file with optional 1-based line slicing; nil
// when it does not exist.
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

// WriteFile writes a file, creating parent directories.
func (s *Session) WriteFile(_ context.Context, path string, content io.Reader) error {
	target := s.resolve(path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// WriteBinaryFile writes bytes to a file.
func (s *Session) WriteBinaryFile(ctx context.Context, path string, content []byte) error {
	return s.WriteFile(ctx, path, bytes.NewReader(content))
}

// WriteTextFile writes UTF-8 text to a file.
func (s *Session) WriteTextFile(ctx context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	return s.WriteFile(ctx, opts.Path, strings.NewReader(opts.Content))
}

// restricted exposes only the providerutils.SandboxSession surface.
type restricted struct{ s *Session }

func (r restricted) Description() string { return r.s.Description() }
func (r restricted) Run(ctx context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	return r.s.Run(ctx, o)
}
func (r restricted) Spawn(ctx context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return r.s.Spawn(ctx, o)
}
func (r restricted) ReadFile(ctx context.Context, p string) (io.ReadCloser, error) {
	return r.s.ReadFile(ctx, p)
}
func (r restricted) ReadBinaryFile(ctx context.Context, p string) ([]byte, error) {
	return r.s.ReadBinaryFile(ctx, p)
}
func (r restricted) ReadTextFile(ctx context.Context, o providerutils.SandboxReadTextFileOptions) (*string, error) {
	return r.s.ReadTextFile(ctx, o)
}
func (r restricted) WriteFile(ctx context.Context, p string, c io.Reader) error {
	return r.s.WriteFile(ctx, p, c)
}
func (r restricted) WriteBinaryFile(ctx context.Context, p string, c []byte) error {
	return r.s.WriteBinaryFile(ctx, p, c)
}
func (r restricted) WriteTextFile(ctx context.Context, o providerutils.SandboxWriteTextFileOptions) error {
	return r.s.WriteTextFile(ctx, o)
}

type process struct {
	cmd    *exec.Cmd
	stdout io.Reader
	stderr io.Reader

	done     chan struct{}
	mu       sync.Mutex
	exitCode int
	exited   bool
	waitErr  error
	killOnce sync.Once
	killErr  error
}

func (p *process) wait(ctx context.Context) {
	err := p.cmd.Wait()
	code := -1
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil && code != 0 {
		err = ctxErr
	}
	p.mu.Lock()
	p.exitCode, p.exited, p.waitErr = code, true, err
	p.mu.Unlock()
	close(p.done)
}

func (p *process) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *process) Stdout() io.Reader { return p.stdout }
func (p *process) Stderr() io.Reader { return p.stderr }

func (p *process) Wait() (providerutils.SandboxProcessResult, error) {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return providerutils.SandboxProcessResult{ExitCode: p.exitCode}, p.waitErr
}

// Kill terminates the whole process group. Safe to call more than once.
func (p *process) Kill() error {
	p.killOnce.Do(func() {
		p.mu.Lock()
		exited := p.exited
		p.mu.Unlock()
		if exited || p.cmd.Process == nil {
			return
		}
		p.killErr = killProcessGroup(p.cmd)
	})
	return p.killErr
}

func (p *process) ExitCode() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode, p.exited
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

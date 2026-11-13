package vercel

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// posixDir is Node's `path.posix.dirname`, used by TS `writeBinaryFile` to
// mkdir -p the parent. Go's path.Dir also cleans the result ("new/../f"
// gives "." where Node gives "new/..", "/a/b/" gives "/a/b" where Node
// gives "/a"), which would skip or change the mkdir.
func posixDir(p string) string { return posixpath.Dirname(p) }

// Session is a providerutils.SandboxSession backed by a Vercel Sandbox. It is
// the tool-safe surface (file I/O, exec, spawn) — what
// NetworkSession.Restricted() returns. Ports
// packages/sandbox-vercel/src/vercel-sandbox-session.ts
// (VercelSandboxSession).
type Session struct {
	sandbox *Sandbox
}

var (
	_ providerutils.SandboxSession         = (*Session)(nil)
	_ providerutils.SandboxStatter         = (*Session)(nil)
	_ providerutils.SandboxDirectoryLister = (*Session)(nil)
	_ providerutils.SandboxFileDeleter     = (*Session)(nil)
)

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
	// TS abortSignal?.throwIfAborted(): the abort reason, before any request.
	if ctx.Err() != nil {
		return providerutils.SandboxRunResult{}, context.Cause(ctx)
	}
	result, err := s.sandbox.RunCommand(ctx, "bash", []string{"-c", opts.Command}, s.resolveWorkingDirectory(opts.WorkingDirectory), opts.Env)
	return providerutils.SandboxRunResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode}, err
}

// resolveWorkingDirectory makes a relative working directory absolute
// against the session cwd: the Vercel API resolves a relative cwd against
// "/" instead (TS resolveWorkingDirectory, 5c247e9b85). The paths are joined
// as text, not cleaned, so symbolic links are followed.
func (s *Session) resolveWorkingDirectory(workingDirectory string) string {
	if workingDirectory == "" || strings.HasPrefix(workingDirectory, "/") {
		return workingDirectory
	}
	cwd := s.sandbox.CWD()
	if cwd == "" || cwd == "/" {
		return "/" + workingDirectory
	}
	return cwd + "/" + workingDirectory
}

// Spawn starts opts.Command via `bash -c`, detached, streaming output live.
// Cancelling ctx kills the remote command, as the TS abort listener does
// (269f4c0c32); Wait then returns the context error.
//
// With opts.Stdin the command is started with attachStdin and the returned
// process is a providerutils.WritableSandboxProcess whose Stdin writes
// through @vercel/sandbox 3.9.0 writeStdin / closeStdin (TS
// `createStdinStream`).
func (s *Session) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	if ctx.Err() != nil { // TS abortSignal?.throwIfAborted()
		return nil, context.Cause(ctx)
	}
	proc, err := s.sandbox.spawnCommand(ctx, "bash", []string{"-c", opts.Command}, s.resolveWorkingDirectory(opts.WorkingDirectory), opts.Env, opts.Stdin)
	if err != nil {
		return nil, err
	}
	a := &sandboxProcessAdapter{ctx: ctx, proc: proc}
	a.stopKillOnCancel = context.AfterFunc(ctx, func() { _ = a.Kill() })
	if opts.Stdin {
		return &writableSandboxProcessAdapter{sandboxProcessAdapter: a}, nil
	}
	return a, nil
}

// writableSandboxProcessAdapter is a process spawned with opts.Stdin.
type writableSandboxProcessAdapter struct {
	*sandboxProcessAdapter
}

// Stdin implements providerutils.WritableSandboxProcess.
func (w *writableSandboxProcessAdapter) Stdin() io.WriteCloser { return w.proc.Stdin() }

var _ providerutils.WritableSandboxProcess = (*writableSandboxProcessAdapter)(nil)

// sandboxProcessAdapter adapts *RemoteProcess (whose Wait/Kill take a
// context) to the context-free providerutils.SandboxProcess interface, using
// the context Spawn was called with — mirroring the TS closure that captures
// `abortSignal` at spawn time.
type sandboxProcessAdapter struct {
	ctx  context.Context
	proc *RemoteProcess
	// stopKillOnCancel removes the kill-on-cancel hook once the command has
	// exited or was killed explicitly.
	stopKillOnCancel func() bool
}

func (a *sandboxProcessAdapter) PID() int              { return a.proc.PID() }
func (a *sandboxProcessAdapter) Stdout() io.Reader     { return a.proc.Stdout() }
func (a *sandboxProcessAdapter) Stderr() io.Reader     { return a.proc.Stderr() }
func (a *sandboxProcessAdapter) ExitCode() (int, bool) { return a.proc.ExitCode() }

func (a *sandboxProcessAdapter) Wait() (providerutils.SandboxProcessResult, error) {
	code, err := a.proc.Wait(a.ctx)
	if err != nil {
		if a.ctx.Err() != nil { // TS abortSignal?.throwIfAborted()
			return providerutils.SandboxProcessResult{}, context.Cause(a.ctx)
		}
		return providerutils.SandboxProcessResult{}, err
	}
	if a.stopKillOnCancel != nil {
		a.stopKillOnCancel()
	}
	return providerutils.SandboxProcessResult{ExitCode: code}, nil
}

// Kill terminates the command. TS calls `command.kill()` without the spawn
// abort signal, so a kill after the spawn context was cancelled (the usual
// cleanup order) must still reach the sandbox.
func (a *sandboxProcessAdapter) Kill() error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 30*time.Second)
	defer cancel()
	err := a.proc.Kill(ctx)
	if a.stopKillOnCancel != nil {
		a.stopKillOnCancel()
	}
	return err
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
	if ctx.Err() != nil { // TS abortSignal?.throwIfAborted()
		return nil, context.Cause(ctx)
	}
	data, err := s.sandbox.ReadFileToBuffer(ctx, path, "")
	if err != nil && ctx.Err() == nil && isFileNotFoundError(err) {
		return nil, nil
	}
	return data, err
}

// fileNotFoundMessage is TS isFileNotFoundError's message test,
// /no such file|not found|does not exist|ENOENT/i: a JS /i regexp without
// the u flag never folds s to U+017F (Go (?i) does).
var fileNotFoundMessage = regexp.MustCompile(`(?i)no (?-i:[sS])uch file|not found|doe(?-i:[sS]) not exi(?-i:[sS])t|ENOENT`)

// isFileNotFoundError is TS VercelSandboxSession isFileNotFoundError: an
// error whose message says the file is missing reads as a missing file.
func isFileNotFoundError(err error) bool {
	return fileNotFoundMessage.MatchString(err.Error())
}

// ReadTextFile reads a UTF-8 file with optional 1-based line slicing.
func (s *Session) ReadTextFile(ctx context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	data, err := s.ReadBinaryFile(ctx, opts.Path)
	if err != nil || data == nil {
		return nil, err
	}
	text, err := providerutils.DecodeSandboxText(data, opts.Encoding)
	if err != nil {
		return nil, err
	}
	text = providerutils.ExtractLines(text, opts.StartLine, opts.EndLine)
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
	if ctx.Err() != nil { // TS abortSignal?.throwIfAborted()
		return context.Cause(ctx)
	}
	if parent := posixDir(path); parent != "" && parent != "." && parent != "/" {
		if _, err := s.sandbox.RunCommand(ctx, "mkdir", []string{"-p", parent}, "", nil); err != nil {
			return err
		}
	}
	return s.sandbox.WriteFiles(ctx, []WriteFileInput{{Path: path, Content: content}}, s.sandbox.CWD())
}

// WriteTextFile writes UTF-8 text to a path.
func (s *Session) WriteTextFile(ctx context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	data, err := providerutils.EncodeSandboxText(opts.Content, opts.Encoding)
	if err != nil {
		return err
	}
	return s.WriteBinaryFile(ctx, opts.Path, data)
}

// Exit codes of the path scripts, distinct from those bash and coreutils use
// for their own errors (TS NOT_FOUND_EXIT_CODE / WRONG_TYPE_EXIT_CODE).
const (
	pathScriptNotFound  = 3
	pathScriptWrongType = 4
)

// The path methods run these scripts (copied from TS
// vercel-sandbox-session.ts, 5c247e9b85) instead of the native file API,
// which reports every error other than "No such file or directory" as
// EACCES and passes paths without `--`. The path is passed as $1.
const exitMissingFunction = `
exit_missing() {
  error=$(LC_ALL=C; : 2>&1 < "$1")
  case $error in
    *": No such file or directory" | *": Not a directory") exit 3 ;;
  esac
  echo "${error:-Cannot access $1}" >&2
  exit 1
}
`

const statScript = exitMissingFunction + `
[ -e "$1" ] || exit_missing "$1"
if [ -f "$1" ]; then type=file; elif [ -d "$1" ]; then type=directory; else type=other; fi
size=$(stat -L -c %s -- "$1" 2>/dev/null || stat -L -f %z -- "$1") || exit 1
printf '%s %s' "$type" "$size"
`

const listDirectoryScript = exitMissingFunction + `
if [ ! -d "$1" ]; then
  [ -e "$1" ] || exit_missing "$1"
  echo "Not a directory" >&2
  exit 4
fi
case $1 in /*) dir=$1 ;; *) dir=./$1 ;; esac
cd -P -- "$dir" || exit 1
[ -r . ] || { echo "Permission denied" >&2; exit 1; }
shopt -s dotglob nullglob
for name in *; do
  if [ -L "$name" ]; then type=symlink
  elif [ -f "$name" ]; then type=file
  elif [ -d "$name" ]; then type=directory
  else type=other
  fi
  printf '%s %s\0' "$type" "$name"
done
`

const deleteFileScript = exitMissingFunction + `
if [ -d "$1" ] && [ ! -L "$1" ]; then
  echo "Is a directory" >&2
  exit 4
fi
[ -e "$1" ] || [ -L "$1" ] || exit_missing "$1"
rm -f -- "$1"
`

func (s *Session) runPathScript(ctx context.Context, script, filePath string) (RunResult, error) {
	if ctx.Err() != nil { // TS abortSignal?.throwIfAborted()
		return RunResult{}, context.Cause(ctx)
	}
	return s.sandbox.RunCommand(ctx, "bash", []string{"-c", script, "bash", filePath}, "", nil)
}

// Stat implements providerutils.SandboxStatter (TS VercelSandboxSession.stat).
func (s *Session) Stat(ctx context.Context, filePath string) (*providerutils.SandboxFileStat, error) {
	res, err := s.runPathScript(ctx, statScript, filePath)
	if err != nil {
		return nil, err
	}
	if res.ExitCode == pathScriptNotFound {
		return nil, nil
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("Failed to stat %s: %s", filePath, providerutils.JSTrim(res.Stderr)) //nolint:staticcheck // TS error text
	}
	typ, rawSize, _ := strings.Cut(res.Stdout, " ")
	size, _ := providerutils.JSNumber(rawSize)
	if math.IsNaN(size) { // TS Number() of a non-number
		size = 0
	}
	return &providerutils.SandboxFileStat{Type: typ, Size: int64(size)}, nil
}

// ListDirectory implements providerutils.SandboxDirectoryLister (TS
// VercelSandboxSession.listDirectory).
func (s *Session) ListDirectory(ctx context.Context, filePath string) ([]providerutils.SandboxDirectoryEntry, error) {
	res, err := s.runPathScript(ctx, listDirectoryScript, filePath)
	if err != nil {
		return nil, err
	}
	if res.ExitCode == pathScriptNotFound {
		return nil, nil
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("Failed to list %s: %s", filePath, providerutils.JSTrim(res.Stderr)) //nolint:staticcheck // TS error text
	}
	entries := []providerutils.SandboxDirectoryEntry{}
	for _, record := range strings.Split(res.Stdout, "\x00") {
		if record == "" {
			continue
		}
		// TS splits at record.indexOf(' '); without a space (-1) the name is
		// the whole record and the type is all but its last character.
		typ, name, ok := strings.Cut(record, " ")
		if !ok {
			units := utf16.Encode([]rune(record))
			typ, name = string(utf16.Decode(units[:len(units)-1])), record
		}
		entries = append(entries, providerutils.SandboxDirectoryEntry{Name: name, Type: typ})
	}
	return entries, nil
}

// DeleteFile implements providerutils.SandboxFileDeleter (TS
// VercelSandboxSession.deleteFile).
func (s *Session) DeleteFile(ctx context.Context, filePath string) (bool, error) {
	res, err := s.runPathScript(ctx, deleteFileScript, filePath)
	if err != nil {
		return false, err
	}
	switch res.ExitCode {
	case 0:
		return true, nil
	case pathScriptNotFound:
		return false, nil
	case pathScriptWrongType:
		return false, fmt.Errorf("Cannot delete %s: it is a directory.", filePath) //nolint:staticcheck // TS error text
	}
	return false, fmt.Errorf("Failed to delete %s: %s", filePath, providerutils.JSTrim(res.Stderr)) //nolint:staticcheck // TS error text
}

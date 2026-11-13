package providerutils

import (
	"context"
	"io"
)

// SandboxSession executes commands and reads/writes files in an isolated
// environment.
//
// Relative paths in every method, including the WorkingDirectory option of
// Run and Spawn, resolve against the same base directory: the directory Run
// and Spawn start in when no WorkingDirectory is given. Absolute paths are
// used as is (TS sandbox.ts, 5c247e9b85).
//
// TS 5c247e9b85 added stat, listDirectory and deleteFile, and 95348b32c6 a
// writable stdin for spawn. Adding methods here would break existing
// implementations, so they are optional interfaces: SandboxStatter,
// SandboxDirectoryLister, SandboxFileDeleter and WritableSandboxProcess
// (with SandboxProcessOptions.Stdin).
type SandboxSession interface {
	Description() string
	Run(ctx context.Context, opts SandboxProcessOptions) (SandboxRunResult, error)
	Spawn(ctx context.Context, opts SandboxProcessOptions) (SandboxProcess, error)
	ReadFile(ctx context.Context, path string) (io.ReadCloser, error)
	ReadBinaryFile(ctx context.Context, path string) ([]byte, error)
	ReadTextFile(ctx context.Context, opts SandboxReadTextFileOptions) (*string, error)
	WriteFile(ctx context.Context, path string, content io.Reader) error
	WriteBinaryFile(ctx context.Context, path string, content []byte) error
	WriteTextFile(ctx context.Context, opts SandboxWriteTextFileOptions) error
}

// Experimental_SandboxSession mirrors the TypeScript SDK's exported alias.
// SandboxSession remains the canonical Go name in providerutils.
type Experimental_SandboxSession = SandboxSession

// SandboxProcessOptions are passed to SandboxSession.Run and SandboxSession.Spawn.
type SandboxProcessOptions struct {
	Command          string
	WorkingDirectory string
	// Env contains per-command environment variables. It mirrors the TypeScript
	// SandboxSession env option.
	Env map[string]string
	// Stdin, for Spawn only, keeps standard input open: the returned process
	// then implements WritableSandboxProcess. An implementation that does not
	// support a writable stdin must return an error before starting the
	// command. When false, standard input is closed (TS spawn `stdin`,
	// 95348b32c6). Run ignores it.
	Stdin bool
}

// WritableSandboxProcess is a SandboxProcess spawned with
// SandboxProcessOptions.Stdin set. Mirrors TS `WritableSandboxProcess`.
type WritableSandboxProcess interface {
	SandboxProcess
	// Stdin is written to the process's standard input. Closing it delivers
	// EOF after pending writes without terminating the process.
	Stdin() io.WriteCloser
}

// Sandbox file and directory types reported by SandboxStatter and
// SandboxDirectoryLister. Mirrors TS `SandboxFileStat` / `SandboxDirectoryEntry`.
const (
	SandboxPathTypeFile      = "file"
	SandboxPathTypeDirectory = "directory"
	SandboxPathTypeSymlink   = "symlink"
	SandboxPathTypeOther     = "other"
)

// SandboxFileStat describes a file or directory. Mirrors TS `SandboxFileStat`.
type SandboxFileStat struct {
	// Type is "file", "directory" or "other" (devices, sockets, named pipes).
	Type string
	// Size is the size in bytes; a directory's size is implementation-defined.
	Size int64
}

// SandboxDirectoryEntry is one entry of a directory listing. Mirrors TS
// `SandboxDirectoryEntry`.
type SandboxDirectoryEntry struct {
	// Name is the entry name, without the directory path.
	Name string
	// Type is "file", "directory", "symlink" (not followed) or "other".
	Type string
}

// SandboxStatter is an optional SandboxSession extension (TS
// `SandboxSession.stat`).
type SandboxStatter interface {
	// Stat reports a path, following symbolic links. It returns (nil, nil)
	// when the path does not exist.
	Stat(ctx context.Context, path string) (*SandboxFileStat, error)
}

// SandboxDirectoryLister is an optional SandboxSession extension (TS
// `SandboxSession.listDirectory`).
type SandboxDirectoryLister interface {
	// ListDirectory lists one directory without descending into
	// subdirectories, in unspecified order. It returns (nil, nil) when the
	// directory does not exist and an error when the path is not a directory.
	ListDirectory(ctx context.Context, path string) ([]SandboxDirectoryEntry, error)
}

// SandboxFileDeleter is an optional SandboxSession extension (TS
// `SandboxSession.deleteFile`).
type SandboxFileDeleter interface {
	// DeleteFile deletes one file (a symbolic link itself, not its target).
	// It returns true when the file was deleted, false when it does not
	// exist, and an error when the path is a directory.
	DeleteFile(ctx context.Context, path string) (bool, error)
}

// SandboxRunResult is returned by SandboxSession.Run.
type SandboxRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SandboxReadTextFileOptions controls text file reads.
type SandboxReadTextFileOptions struct {
	Path      string
	Encoding  string
	StartLine *int
	EndLine   *int
}

// SandboxWriteTextFileOptions controls text file writes.
type SandboxWriteTextFileOptions struct {
	Path     string
	Content  string
	Encoding string
}

// SandboxProcess is a handle to a long-running sandbox process.
type SandboxProcess interface {
	// PID returns the process identifier when the sandbox exposes one.
	PID() int

	// Stdout streams bytes written by the process to standard output.
	Stdout() io.Reader

	// Stderr streams bytes written by the process to standard error.
	Stderr() io.Reader

	// Wait blocks until the process exits and returns its exit code.
	Wait() (SandboxProcessResult, error)

	// Kill terminates the process. It is safe to call more than once.
	Kill() error

	// ExitCode returns the exit code once known. The boolean is false until the
	// process has exited.
	ExitCode() (int, bool)
}

// Experimental_SandboxProcess mirrors the TypeScript SDK's exported
// Experimental_SandboxProcess alias. SandboxProcess remains the canonical Go
// name.
type Experimental_SandboxProcess = SandboxProcess

// SandboxProcessResult is returned by SandboxProcess.Wait.
type SandboxProcessResult struct {
	ExitCode int
}

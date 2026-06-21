package providerutils

import (
	"context"
	"io"
)

// SandboxSession executes commands and reads/writes files in an isolated
// environment.
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

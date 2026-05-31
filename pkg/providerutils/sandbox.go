package providerutils

import "io"

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

// SandboxProcessResult is returned by SandboxProcess.Wait.
type SandboxProcessResult struct {
	ExitCode int
}

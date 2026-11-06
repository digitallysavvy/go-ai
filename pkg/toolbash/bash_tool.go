package toolbash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const (
	// ToolName is the name of the tool.
	ToolName = "bash"

	// DefaultMaxOutputLength is the default number of characters kept from
	// each output stream.
	DefaultMaxOutputLength = 30_000

	// MaxTimeoutMs is the largest accepted timeout (TS: 2_147_483_647).
	MaxTimeoutMs = 2_147_483_647

	// TimeoutExitCode is the exit code reported when a command is stopped by
	// the timeout (the `timeout(1)` convention).
	TimeoutExitCode = 124

	// ioGrace bounds how long the tool waits for a killed or exited process
	// and for its output pipes to drain.
	ioGrace = 2 * time.Second
)

// Options configures CreateBashTool.
type Options struct {
	// Sandbox is the session used for commands and file operations. Required.
	// The caller owns its lifecycle; tools using the same session share its
	// files. Available commands and persistence depend on the sandbox.
	Sandbox providerutils.SandboxSession

	// WorkingDirectory is an optional absolute starting directory for every
	// command, created if needed. When empty, the sandbox's default
	// directory is used.
	WorkingDirectory string

	// MaxOutputLength is the maximum number of characters kept from each
	// output stream before a truncation notice is appended. Nil uses
	// DefaultMaxOutputLength. Must be positive.
	MaxOutputLength *int

	// TimeoutMs is an optional timeout in milliseconds for each command
	// execution; it restarts for every command. Nil means no timeout. Must be
	// between 1 and MaxTimeoutMs. Cancellation of the call's context is
	// honored as well and returns the context's error.
	TimeoutMs *int
}

// Output is the tool's result.
type Output struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// CreateBashTool creates a provider-independent bash tool backed by
// opts.Sandbox. It validates the options before touching the sandbox, then
// creates opts.WorkingDirectory in the sandbox when set.
//
// Experimental: mirrors TS experimental_createBashTool and may change.
func CreateBashTool(ctx context.Context, opts Options) (types.Tool, error) {
	if opts.Sandbox == nil {
		return types.Tool{}, &providerrors.InvalidArgumentError{Field: "sandbox", Message: "A sandbox session is required."}
	}
	if opts.TimeoutMs != nil && (*opts.TimeoutMs < 1 || *opts.TimeoutMs > MaxTimeoutMs) {
		return types.Tool{}, &providerrors.InvalidArgumentError{
			Field:   "timeout",
			Message: "Must be an integer between 1 and 2147483647 milliseconds.",
		}
	}
	maxOutput := DefaultMaxOutputLength
	if opts.MaxOutputLength != nil {
		maxOutput = *opts.MaxOutputLength
		if maxOutput < 1 || int64(maxOutput) > maxSafeInteger {
			return types.Tool{}, &providerrors.InvalidArgumentError{Field: "maxOutputLength", Message: "Must be a positive safe integer."}
		}
	}
	workingDirectory := opts.WorkingDirectory
	if workingDirectory != "" {
		if !strings.HasPrefix(workingDirectory, "/") {
			return types.Tool{}, &providerrors.InvalidArgumentError{Field: "workingDirectory", Message: "Must be an absolute path in the sandbox."}
		}
		workingDirectory = normalizePosix(workingDirectory)

		result, err := opts.Sandbox.Run(ctx, providerutils.SandboxProcessOptions{
			Command: "mkdir -p -- '" + strings.ReplaceAll(workingDirectory, "'", `'\''`) + "'",
		})
		if err != nil {
			return types.Tool{}, err
		}
		if result.ExitCode != 0 {
			return types.Tool{}, fmt.Errorf("Failed to create working directory: %s", result.Stderr) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}

	var timeout time.Duration
	if opts.TimeoutMs != nil {
		timeout = time.Duration(*opts.TimeoutMs) * time.Millisecond
	}
	b := &bashTool{sandbox: opts.Sandbox, workingDirectory: workingDirectory, maxOutput: maxOutput, timeout: timeout}

	startsIn := "Commands run in the sandbox's default working directory. "
	if workingDirectory != "" {
		startsIn = "Every command starts in " + workingDirectory + ". "
	}
	return types.Tool{
		Name: ToolName,
		Description: "Run Bash commands in the sandbox. " + startsIn +
			"Use ls to discover files and commands such as cat, grep, sort, awk, " +
			"and jq to inspect and transform them. Pipes and redirections are supported. " +
			"Returns stdout, stderr, and exitCode; long output is truncated.\n" +
			opts.Sandbox.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The Bash command or script to execute.",
				},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
		OutputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"stdout":   map[string]interface{}{"type": "string"},
				"stderr":   map[string]interface{}{"type": "string"},
				"exitCode": map[string]interface{}{"type": "number"},
			},
			"required":             []string{"stdout", "stderr", "exitCode"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			command, ok := input["command"].(string)
			if !ok {
				return nil, fmt.Errorf("bash: input.command must be a string")
			}
			return b.execute(ctx, command)
		},
	}, nil
}

// normalizePosix is path.posix.normalize: Clean, but a trailing slash is kept
// (except for the root).
func normalizePosix(p string) string {
	cleaned := path.Clean(p)
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		return cleaned + "/"
	}
	return cleaned
}

type bashTool struct {
	sandbox          providerutils.SandboxSession
	workingDirectory string
	maxOutput        int
	timeout          time.Duration
}

func (b *bashTool) execute(ctx context.Context, command string) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	runCtx := ctx
	if b.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	proc, err := b.sandbox.Spawn(runCtx, providerutils.SandboxProcessOptions{
		Command:          command,
		WorkingDirectory: b.workingDirectory,
	})
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return Output{}, cerr
		}
		return Output{}, err
	}

	stdout, stderr := newBoundedOutput(b.maxOutput), newBoundedOutput(b.maxOutput)
	var readers sync.WaitGroup
	drain := func(r io.Reader, dst *boundedOutput) {
		defer readers.Done()
		_, _ = io.Copy(dst, r)
	}
	readers.Add(2)
	go drain(proc.Stdout(), stdout)
	go drain(proc.Stderr(), stderr)

	type waitResult struct {
		res providerutils.SandboxProcessResult
		err error
	}
	waited := make(chan waitResult, 1)
	go func() {
		res, werr := proc.Wait()
		waited <- waitResult{res, werr}
	}()

	var result waitResult
	timedOut := false
	select {
	case result = <-waited:
	case <-runCtx.Done():
		// Kill the process (its whole group on sandboxes that use one)
		// instead of relying on the sandbox to notice the cancellation.
		_ = proc.Kill()
		select {
		case result = <-waited:
		case <-time.After(ioGrace):
		}
		if ctx.Err() == nil {
			timedOut = true
		}
	}
	if cerr := ctx.Err(); cerr != nil {
		_ = proc.Kill()
		return Output{}, cerr
	}

	// Output pipes can stay open when a background child inherited them:
	// give the readers a moment, then close the pipes if the sandbox allows.
	drained := make(chan struct{})
	go func() { readers.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(ioGrace):
		closeIfCloser(proc.Stdout())
		closeIfCloser(proc.Stderr())
	}

	exitCode := result.res.ExitCode
	switch {
	case timedOut:
		exitCode = TimeoutExitCode
	case result.err != nil && !errors.Is(result.err, context.DeadlineExceeded) && !errors.Is(result.err, context.Canceled):
		return Output{}, result.err
	}
	return Output{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}, nil
}

func closeIfCloser(r io.Reader) {
	if c, ok := r.(io.Closer); ok {
		_ = c.Close()
	}
}

// boundedOutput keeps the first maxChars characters written to it and counts
// the rest, discarding them as they stream. String appends TS's truncation
// notice when characters were dropped.
type boundedOutput struct {
	mu       sync.Mutex
	maxChars int
	maxBytes int
	buf      []byte
	total    int64 // characters seen (UTF-8 sequence starts); int64 so it cannot wrap where int is 32 bits
}

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER; TS rejects a larger
// maxOutputLength (Number.isSafeInteger). It is an int64 so the package builds
// where int is 32 bits.
const maxSafeInteger int64 = 1<<53 - 1

// maxScaledChars is the largest maxChars whose byte budget is
// maxChars*utf8.UTFMax: 2^60 on 64-bit, and math.MaxInt/utf8.UTFMax where int
// is 32 bits so the product cannot overflow. Larger limits use maxChars bytes.
const maxScaledChars = min(int64(1<<62)/utf8.UTFMax, math.MaxInt/utf8.UTFMax)

func newBoundedOutput(maxChars int) *boundedOutput {
	maxBytes := maxChars
	if int64(maxChars) <= maxScaledChars {
		maxBytes = maxChars * utf8.UTFMax
	}
	return &boundedOutput{maxChars: maxChars, maxBytes: maxBytes}
}

func (o *boundedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, c := range p {
		if c&0xC0 != 0x80 {
			o.total++
		}
	}
	if room := o.maxBytes - len(o.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		o.buf = append(o.buf, p[:room]...)
	}
	return len(p), nil
}

func (o *boundedOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.total <= int64(o.maxChars) {
		return string(o.buf)
	}
	text, seen := o.buf, 0
	end := len(text)
	for i := range string(text) {
		if seen == o.maxChars {
			end = i
			break
		}
		seen++
	}
	return fmt.Sprintf("%s\n[Output truncated: %d characters omitted.]", text[:end], o.total-int64(o.maxChars))
}

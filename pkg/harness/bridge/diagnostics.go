package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultTailLimit is the number of trailing process output lines kept for
// startup error reports.
const DefaultTailLimit = 20

// LineTail is a concurrency-safe ring of the most recent output lines of a
// bridge process (TS `collectTail` arrays, which Go must guard because the
// forwarder runs on its own goroutine).
type LineTail struct {
	mu    sync.Mutex
	limit int
	lines []string
}

// NewLineTail returns a tail keeping at most limit lines (DefaultTailLimit
// when limit <= 0).
func NewLineTail(limit int) *LineTail {
	if limit <= 0 {
		limit = DefaultTailLimit
	}
	return &LineTail{limit: limit}
}

// Push appends a line, dropping the oldest beyond the limit.
func (t *LineTail) Push(line string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.limit <= 0 {
		t.limit = DefaultTailLimit
	}
	t.lines = append(t.lines, line)
	if n := len(t.lines) - t.limit; n > 0 {
		t.lines = append([]string(nil), t.lines[n:]...)
	}
}

// Lines returns a snapshot of the retained lines.
func (t *LineTail) Lines() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.lines...)
}

// FormatBridgeError renders an error value for logs. Go errors render as
// their message; serialized bridge errors (a *harness.DiagnosticError, or a
// JSON object with a string `message`) render as `stack`, else
// `name: message`; anything else is JSON-stringified. Mirrors TS
// `formatBridgeError` (39c8276).
func FormatBridgeError(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case *harness.DiagnosticError:
		if v != nil {
			return formatSerialized(v.Name, v.Message, v.Stack)
		}
	case harness.DiagnosticError:
		return formatSerialized(v.Name, v.Message, v.Stack)
	case error:
		return v.Error()
	case map[string]any:
		if msg, ok := v["message"].(string); ok {
			name, _ := v["name"].(string)
			stack, _ := v["stack"].(string)
			return formatSerialized(name, msg, stack)
		}
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(v, &decoded) == nil {
			return FormatBridgeError(decoded)
		}
		return string(v)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}

func formatSerialized(name, message, stack string) string {
	if stack != "" {
		return stack
	}
	if name != "" {
		return name + ": " + message
	}
	return message
}

// LogBridgeErrorOptions is the input of LogBridgeError.
type LogBridgeErrorOptions struct {
	HarnessID string
	SessionID string
	Context   string
	Error     any
	// Write receives each formatted line (newline-terminated). Defaults to
	// os.Stderr.
	Write func(line string)
}

// LogBridgeError writes an error, one prefixed line per non-blank line:
// `[harness:<id>:error session=<sid>] <context>: <error>`. Mirrors TS
// `logBridgeError`.
func LogBridgeError(opts LogBridgeErrorOptions) {
	write := opts.Write
	if write == nil {
		write = writeToStderr
	}
	prefix := "[harness:" + opts.HarnessID + ":error"
	if opts.SessionID != "" {
		prefix += " session=" + opts.SessionID
	}
	prefix += "]"
	message := FormatBridgeError(opts.Error)
	if opts.Context != "" {
		message = opts.Context + ": " + message
	}
	for _, line := range strings.Split(message, "\n") {
		if strings.TrimSpace(line) != "" {
			write(prefix + " " + line + "\n")
		}
	}
}

// CreateBridgeErrorHandler returns a Channel OnBridgeError handler that logs
// bridge `error` frames to stderr. Mirrors TS `createBridgeErrorHandler`.
func CreateBridgeErrorHandler(harnessID, sessionID string) func(*harness.ErrorPart) {
	return func(part *harness.ErrorPart) {
		var value any
		if part != nil {
			value = part.Error
		}
		LogBridgeError(LogBridgeErrorOptions{
			HarnessID: harnessID,
			SessionID: sessionID,
			Context:   "bridge emitted an error frame",
			Error:     value,
		})
	}
}

// StartupErrorOptions is the input of CreateBridgeStartupError.
type StartupErrorOptions struct {
	Message    string
	Proc       providerutils.SandboxProcess
	StdoutTail []string
	StderrTail *LineTail
	// StderrDone, when set, is awaited (up to 250ms) so the stderr tail is
	// complete before it is reported.
	StderrDone <-chan struct{}
}

// startupWaitGrace bounds each of the stderr and exit-code waits.
const startupWaitGrace = 250 * time.Millisecond

// CreateBridgeStartupError builds the error reported when a bridge fails to
// become ready: the message, the exit code when the process exits within
// 250ms, and the stdout/stderr tails. Mirrors TS `createBridgeStartupError`
// (39c8276).
func CreateBridgeStartupError(opts StartupErrorOptions) error {
	if opts.StderrDone != nil {
		select {
		case <-opts.StderrDone:
		case <-time.After(startupWaitGrace):
		}
	}

	exitStatus := ""
	if opts.Proc != nil {
		if code, ok := processExitCode(opts.Proc, startupWaitGrace); ok {
			exitStatus = fmt.Sprintf(" Exit code: %d.", code)
		}
	}

	var details []string
	if len(opts.StdoutTail) > 0 {
		details = append(details, "stdout:\n"+strings.Join(opts.StdoutTail, "\n"))
	}
	if stderr := opts.StderrTail.Lines(); len(stderr) > 0 {
		details = append(details, "stderr:\n"+strings.Join(stderr, "\n"))
	}
	msg := opts.Message + exitStatus
	if len(details) > 0 {
		msg += "\n\n" + strings.Join(details, "\n\n")
	}
	return errors.New(msg)
}

func processExitCode(proc providerutils.SandboxProcess, grace time.Duration) (int, bool) {
	if code, ok := proc.ExitCode(); ok {
		return code, true
	}
	type result struct {
		code int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := proc.Wait()
		ch <- result{r.ExitCode, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return 0, false
		}
		return r.code, true
	case <-time.After(grace):
		return 0, false
	}
}

// ForwardOptions configures ForwardBridgeProcessStream.
type ForwardOptions struct {
	// StreamName is "stdout" or "stderr".
	StreamName string
	// Source labels forwarded lines (default "bridge").
	Source string
	// Tail, when set, collects the most recent lines.
	Tail *LineTail
	// Write receives each forwarded line; defaults to os.Stderr.
	Write func(line string)
}

// ForwardBridgeProcessStream forwards each non-blank line of r to stderr as
// `[harness:<source>:<stream>] <line>` on a background goroutine and returns
// a channel closed once r is exhausted. Mirrors TS
// `forwardBridgeProcessStream`.
func ForwardBridgeProcessStream(r io.Reader, opts ForwardOptions) <-chan struct{} {
	done := make(chan struct{})
	source := opts.Source
	if source == "" {
		source = "bridge"
	}
	write := opts.Write
	if write == nil {
		write = writeToStderr
	}
	go func() {
		defer close(done)
		if r == nil {
			return
		}
		decoder := &lineDecoder{}
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			var lines []string
			if n > 0 {
				lines = decoder.push(string(buf[:n]))
			}
			if err != nil {
				lines = append(lines, decoder.flush()...)
			}
			for _, line := range lines {
				opts.Tail.Push(line)
				write("[harness:" + source + ":" + opts.StreamName + "] " + line + "\n")
			}
			if err != nil {
				return
			}
		}
	}()
	return done
}

// DrainBridgeProcessStream discards r on a background goroutine so the
// process never blocks on a full pipe, returning a channel closed at EOF.
// Mirrors TS `drainBridgeProcessStream`.
func DrainBridgeProcessStream(r io.Reader) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if r != nil {
			_, _ = io.Copy(io.Discard, r)
		}
	}()
	return done
}

func writeToStderr(line string) { _, _ = os.Stderr.WriteString(line) }

// lineDecoder splits a text stream into trimmed, non-empty lines (TS
// `lineDecoder` in bridge-ready.ts / bridge-diagnostics.ts).
type lineDecoder struct{ buffer string }

func (d *lineDecoder) push(chunk string) []string {
	d.buffer += chunk
	var lines []string
	for {
		nl := strings.IndexByte(d.buffer, '\n')
		if nl < 0 {
			return lines
		}
		raw := d.buffer[:nl]
		d.buffer = d.buffer[nl+1:]
		if line := strings.TrimSpace(strings.TrimSuffix(raw, "\r")); line != "" {
			lines = append(lines, line)
		}
	}
}

func (d *lineDecoder) flush() []string {
	line := strings.TrimSpace(strings.TrimSuffix(d.buffer, "\r"))
	d.buffer = ""
	if line == "" {
		return nil
	}
	return []string{line}
}

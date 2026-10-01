package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ReadySource tells where WaitForBridgeReady learned the bound port.
type ReadySource string

const (
	// ReadySourceStdout: the `{"type":"bridge-ready","port":N}` stdout line.
	ReadySourceStdout ReadySource = "stdout"
	// ReadySourceMetadata: `<bridgeStateDir>/bridge-meta.json` in state
	// "waiting" (fallback for runtimes such as Bun whose stdout may not be
	// delivered, aae0138).
	ReadySourceMetadata ReadySource = "metadata"
)

// DefaultReadyPollInterval is the bridge-meta.json poll interval.
const DefaultReadyPollInterval = 100 * time.Millisecond

// ReadyErrorContext is handed to the startup error factories.
type ReadyErrorContext struct {
	Proc       providerutils.SandboxProcess
	StdoutTail []string
}

// WaitForBridgeReadyOptions is the input of WaitForBridgeReady.
type WaitForBridgeReadyOptions struct {
	Proc           providerutils.SandboxProcess
	Sandbox        providerutils.SandboxSession
	BridgeStateDir string
	BridgeType     string
	Timeout        time.Duration
	// PollInterval between bridge-meta.json reads (default 100ms).
	PollInterval time.Duration
	// CreateTimeoutError builds the error returned on timeout (default
	// "bridge did not become ready in time.").
	CreateTimeoutError func(ReadyErrorContext) error
	// CreateExitError builds the error returned when stdout ends first
	// (default "bridge exited before becoming ready.").
	CreateExitError func(ReadyErrorContext) error
}

// ReadyResult is the outcome of WaitForBridgeReady.
type ReadyResult struct {
	Port       int
	Source     ReadySource
	StdoutTail []string
}

// BridgeMetaPath returns `<bridgeStateDir>/bridge-meta.json`.
func BridgeMetaPath(bridgeStateDir string) string { return bridgeStateDir + "/bridge-meta.json" }

// MarkBridgeStarting writes `{"type":<bridgeType>,"state":"starting"}` to
// bridge-meta.json so a stale "waiting" file from a previous bridge cannot
// satisfy the readiness fallback. Best-effort: errors are ignored. Mirrors TS
// `markBridgeStarting`.
func MarkBridgeStarting(ctx context.Context, sandbox providerutils.SandboxSession, bridgeStateDir, bridgeType string) {
	content, _ := json.Marshal(struct {
		Type  string `json:"type"`
		State string `json:"state"`
	}{bridgeType, "starting"})
	_ = sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{
		Path:    BridgeMetaPath(bridgeStateDir),
		Content: string(content),
	})
}

// WaitForBridgeReady waits until the bridge announces its bound port, either
// with the stdout `bridge-ready` line or through bridge-meta.json in state
// "waiting" (polled every PollInterval). On timeout or cancellation the
// process is killed. Once it returns, the rest of stdout is drained in the
// background so the bridge never blocks on a full pipe (TS adapters call
// `drainBridgeProcessStream(proc.stdout)` right after). Mirrors TS
// `waitForBridgeReady` (aae0138).
func WaitForBridgeReady(ctx context.Context, opts WaitForBridgeReadyOptions) (ReadyResult, error) {
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultReadyPollInterval
	}
	deadline := time.Now().Add(opts.Timeout)
	tail := NewLineTail(DefaultTailLimit)

	// done tells the stdout reader to stop reporting and just drain.
	done := make(chan struct{})
	defer close(done)

	type stdoutEvent struct {
		line string
		eof  bool
		err  error
	}
	stdoutEvents := make(chan stdoutEvent)
	go func() {
		send := func(ev stdoutEvent) bool {
			select {
			case stdoutEvents <- ev:
				return true
			case <-done:
				return false
			}
		}
		r := opts.Proc.Stdout()
		if r == nil {
			send(stdoutEvent{eof: true})
			return
		}
		decoder := &lineDecoder{}
		buf := make([]byte, 32*1024)
		reporting := true
		for {
			n, err := r.Read(buf)
			if reporting && n > 0 {
				for _, line := range decoder.push(string(buf[:n])) {
					if !send(stdoutEvent{line: line}) {
						reporting = false
						break
					}
				}
			}
			if err != nil {
				if reporting {
					for _, line := range decoder.flush() {
						// Flushed trailing lines only feed the tail (TS parity).
						tail.Push(line)
					}
					if isEOF(err) {
						send(stdoutEvent{eof: true})
					} else {
						send(stdoutEvent{err: err})
					}
				}
				return
			}
		}
	}()

	metaCtx, cancelMeta := context.WithCancel(ctx)
	defer cancelMeta()
	metaResults := make(chan int, 1)
	metaPending := false
	nextMetaRead := time.Now()

	kill := func() { _ = opts.Proc.Kill() }
	errCtx := func() ReadyErrorContext {
		return ReadyErrorContext{Proc: opts.Proc, StdoutTail: tail.Lines()}
	}

	for {
		if ctx.Err() != nil {
			kill()
			return ReadyResult{}, context.Cause(ctx)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			kill()
			if opts.CreateTimeoutError != nil {
				return ReadyResult{}, opts.CreateTimeoutError(errCtx())
			}
			return ReadyResult{}, errors.New("bridge did not become ready in time.") //nolint:staticcheck // matches TS SDK's exact error text
		}

		if !metaPending && !time.Now().Before(nextMetaRead) {
			metaPending = true
			go func() {
				metaResults <- readBridgeMetaReady(metaCtx, opts.Sandbox, opts.BridgeStateDir, opts.BridgeType)
			}()
		}
		wait := remaining
		if metaPending {
			wait = min(wait, pollInterval)
		} else {
			wait = min(wait, time.Until(nextMetaRead))
		}
		timer := time.NewTimer(max(wait, 0))

		select {
		case <-ctx.Done():
			timer.Stop()
			continue
		case <-timer.C:
			continue
		case port := <-metaResults:
			timer.Stop()
			metaPending = false
			if port > 0 {
				return ReadyResult{Port: port, Source: ReadySourceMetadata, StdoutTail: tail.Lines()}, nil
			}
			nextMetaRead = time.Now().Add(pollInterval)
		case ev := <-stdoutEvents:
			timer.Stop()
			switch {
			case ev.err != nil:
				return ReadyResult{}, ev.err
			case ev.eof:
				if opts.CreateExitError != nil {
					return ReadyResult{}, opts.CreateExitError(errCtx())
				}
				return ReadyResult{}, errors.New("bridge exited before becoming ready.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			tail.Push(ev.line)
			if ready, err := DecodeReady([]byte(ev.line)); err == nil {
				return ReadyResult{Port: ready.Port, Source: ReadySourceStdout, StdoutTail: tail.Lines()}, nil
			}
		}
	}
}

// isEOF reports whether a stdout read error means the stream ended (TS
// `done`): io.EOF or a pipe closed because the process went away.
func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed)
}

type bridgeMeta struct {
	Type  *string  `json:"type"`
	Port  *float64 `json:"port"`
	State *string  `json:"state"`
	PID   *float64 `json:"pid"`
}

// readBridgeMetaReady returns the port from bridge-meta.json when it is in
// state "waiting" for bridgeType, else 0.
func readBridgeMetaReady(ctx context.Context, sandbox providerutils.SandboxSession, bridgeStateDir, bridgeType string) int {
	if sandbox == nil {
		return 0
	}
	raw, err := sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: BridgeMetaPath(bridgeStateDir)})
	if err != nil || raw == nil {
		return 0
	}
	var meta bridgeMeta
	if json.Unmarshal([]byte(*raw), &meta) != nil {
		return 0
	}
	if meta.Type == nil || *meta.Type != bridgeType {
		return 0
	}
	if meta.State == nil || *meta.State != "waiting" {
		return 0
	}
	if meta.Port == nil || *meta.Port <= 0 {
		return 0
	}
	return int(*meta.Port)
}

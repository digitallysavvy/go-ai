package codemode

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// This file wraps a vendored copy of github.com/fastschema/qjs v0.0.6
// (QuickJS compiled to WebAssembly, executed with wazero -- pure Go, no
// cgo), at pkg/internal/third_party/qjs, as the code-mode sandbox engine.
// See that package's README.vendor.md for why it's vendored (memory-safety
// patches to Mem.ReadString/Value.JSONStringify, and the job-queue-
// quiescence exports concurrent approval batching depends on -- see
// QJS_RunPendingJobs/QJS_PromiseState/QJS_PromiseResult, used from
// driveCodeModeExecution in run_code_mode.go, this file's only caller of
// note) and the package doc for how this compares to TypeScript's
// worker+quickjs-emscripten (now `run`) engine.
//
// Two real, verified quirks in fastschema/qjs v0.0.6 shape this file:
//
//  1. The compiled QuickJS module and its wazero RuntimeConfig (including
//     WithCloseOnContextDone) are cached process-globally, keyed only by
//     the wasm bytes, the first time qjs.New is called. A later qjs.New
//     call that passes CloseOnContextDone: true has NO EFFECT if an
//     earlier call in the same process did not. Concretely: without a
//     dedicated warm-up, an infinite JS loop (`while(true){}`) never
//     returns control to Go even with a canceled context. warmUp performs
//     one throwaway qjs.New with CloseOnContextDone: true, using a
//     generous background context so a cold WASM compile is never cut
//     short, before any real invocation runs.
//  2. Once CloseOnContextDone is active, a context deadline firing mid-call
//     makes the library PANIC (not return an error) from whatever qjs.*
//     call is in flight, including on cleanup (Value.Free/Runtime.Close).
//     runInSandbox therefore runs every invocation in its own goroutine
//     with a deferred recover, and never calls Value.Free directly (a
//     fresh Runtime is created per invocation and released in one shot via
//     Runtime.Close, itself panic-guarded).
//
// Both quirks were confirmed empirically against qjs v0.0.6; a future
// version may fix them, at which point the warm-up and panic recovery
// become (harmless) belt-and-suspenders.

var (
	warmUpOnce sync.Once
	warmUpErr  error
)

var (
	maxWorkersMu  sync.Mutex
	maxWorkers    int // 0 (the default) means unlimited.
	activeWorkers int
)

// SetMaxWorkers caps the number of code-mode sandbox invocations that may
// run concurrently, process-wide. 0 (the default) means unlimited. An
// invocation started while the cap is already reached fails immediately
// with *ConcurrencyError rather than queuing. Mirrors TypeScript's
// experimental_setMaxWorkers (from the `run` package, re-exported by
// code-mode); note that TypeScript's cap bounds worker *threads*, while
// this Go port has no worker threads (see the package doc), so it simply
// bounds concurrent in-flight RunCodeMode/Run calls.
//
// Mirrors TypeScript's setMaxWorkers validation ("rejects invalid
// maxWorkers values", code-mode/src/utils/options.test.ts) for negative
// values; unlike TypeScript, n == 0 is accepted (it is Go's "unlimited"
// sentinel here, not an unset/undefined value -- see the doc above and
// resolveExecutionPolicy's doc comment for the same reasoning).
func SetMaxWorkers(n int) error {
	if n < 0 {
		return fmt.Errorf("codemode: maxWorkers must be a positive integer, got %d", n)
	}
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	maxWorkers = n
	return nil
}

// acquireWorkerSlot reserves a concurrency slot for one sandbox invocation,
// returning a release function to call once the invocation completes (or a
// *ConcurrencyError if the process-wide cap set by SetMaxWorkers is
// already reached).
func acquireWorkerSlot() (func(), error) {
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	if maxWorkers > 0 && activeWorkers >= maxWorkers {
		return nil, NewConcurrencyError(maxWorkers)
	}
	activeWorkers++
	return func() {
		maxWorkersMu.Lock()
		activeWorkers--
		maxWorkersMu.Unlock()
	}, nil
}

func warmUp() error {
	warmUpOnce.Do(func() {
		rt, err := qjs.New(qjs.Option{
			MemoryLimit:        DefaultMemoryLimitBytes,
			Context:            context.Background(),
			CloseOnContextDone: true,
		})
		if err != nil {
			warmUpErr = fmt.Errorf("codemode: failed to initialize the QuickJS sandbox: %w", err)
			return
		}
		rt.Close()
	})
	return warmUpErr
}

// sandboxOutcome is the result of one sandbox invocation.
type sandboxOutcome struct {
	resultJSON  string
	isUndefined bool
	interrupted bool
	err         error
}

// sandboxTimeoutGrace bounds how long runInSandbox waits, beyond the
// policy timeout, for the sandbox goroutine to unwind after its context is
// canceled, before giving up and returning to the caller regardless. Go
// cannot forcibly kill a goroutine; this is a defensive backstop so a
// caller's deadline is always honored even if the engine fails to
// interrupt promptly. When that happens the sandbox goroutine is
// abandoned/leaked.
const sandboxTimeoutGrace = 5 * time.Second

// runInSandbox creates a fresh QuickJS sandbox with policy's resource
// limits applied and hands it to drive, which is responsible for
// installing host bindings, evaluating source, and running it to
// completion (or to a quiescent point worth pausing at -- see
// driveCodeModeExecution in run_code_mode.go, drive's only caller).
// runInSandbox itself only owns what every invocation needs regardless of
// what drive does with the context: warm-up, the process-wide concurrency
// slot, the timeout/cancellation goroutine plumbing, and panic recovery
// for the two qjs v0.0.6 quirks documented above.
//
// drive returns (resultJSON, isUndefined, interrupted, err): interrupted
// means drive stopped at a quiescent point with one or more host calls
// newly pending (the caller inspects toolBridge.batchSnapshot for which
// ones), not a completed result -- resultJSON/isUndefined are meaningless
// when interrupted is true.
func runInSandbox(ctx context.Context, policy resolvedPolicy, drive func(jsCtx *qjs.Context) (resultJSON string, isUndefined bool, interrupted bool, err error)) (resultJSON string, isUndefined bool, interrupted bool, err error) {
	if werr := warmUp(); werr != nil {
		return "", false, false, werr
	}

	release, werr := acquireWorkerSlot()
	if werr != nil {
		return "", false, false, werr
	}
	defer release()

	timeout := time.Duration(policy.TimeoutMs) * time.Millisecond
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := make(chan sandboxOutcome, 1)
	go func() {
		var out sandboxOutcome
		defer func() {
			if r := recover(); r != nil {
				out = sandboxOutcome{err: classifySandboxFailure(fmt.Errorf("%v", r), ctx, policy)}
			}
			select {
			case ch <- out:
			default:
			}
		}()

		rt, nerr := qjs.New(qjs.Option{
			MemoryLimit:        policy.MemoryLimitBytes,
			MaxStackSize:       policy.MaxStackSizeBytes,
			MaxExecutionTime:   policy.TimeoutMs,
			Context:            runCtx,
			CloseOnContextDone: true,
		})
		if nerr != nil {
			out.err = classifySandboxFailure(nerr, ctx, policy)
			return
		}
		defer func() {
			defer func() { recover() }()
			rt.Close()
		}()

		jsCtx := rt.Context()
		rJSON, isUndef, interruptedResult, derr := drive(jsCtx)
		if derr != nil {
			out.err = classifySandboxFailure(derr, ctx, policy)
			return
		}
		out.resultJSON = rJSON
		out.isUndefined = isUndef
		out.interrupted = interruptedResult
	}()

	select {
	case out := <-ch:
		return out.resultJSON, out.isUndefined, out.interrupted, out.err
	case <-time.After(timeout + sandboxTimeoutGrace):
		return "", false, false, NewTimeoutError(policy.TimeoutMs)
	}
}

// classifySandboxFailure maps a raw qjs/wazero failure to a CodeModeError
// where the failure is engine-level (aborted/timed out), leaving
// tool-thrown and script-thrown JavaScript errors as plain errors whose
// message is preserved verbatim (RunCodeMode further prefers a preserved
// *typed* CodeModeError captured by the tool bridge, when one caused the
// failure; see toolBridge.lastCodeModeErr).
func classifySandboxFailure(err error, outerCtx context.Context, policy resolvedPolicy) error {
	msg := err.Error()
	if looksLikeContextCancellation(msg) {
		if outerCtx.Err() != nil {
			return NewAbortedError()
		}
		return NewTimeoutError(policy.TimeoutMs)
	}
	return err
}

func looksLikeContextCancellation(msg string) bool {
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "module closed")
}

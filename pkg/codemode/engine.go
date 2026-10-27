package codemode

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"runtime"
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

// Default worker cap, matching the TypeScript `run` package
// (runtime/max-workers.js, run@2.1.8): when SetMaxWorkers has not been
// called, the cap is floor(availableMemory / (memoryLimitBytes + 48 MiB)),
// clamped to [1, 32], and each invocation also reserves that many bytes
// against a memory budget taken when the first reservation is made.
const (
	defaultMaxWorkersCap       = 32
	defaultWorkerOverheadBytes = 48 * 1024 * 1024
)

var (
	maxWorkersMu sync.Mutex
	// maxWorkersSet reports whether SetMaxWorkers configured the cap (TS
	// configuredMaxWorkers !== undefined). maxWorkers is that cap; 0 means
	// unlimited.
	maxWorkersSet bool
	maxWorkers    int
	activeWorkers int
	// memoryBudgetBytes is TS memoryBudgetBytes (0 means unset);
	// reservedMemoryBytes is TS reservedMemoryBytes.
	memoryBudgetBytes   uint64
	reservedMemoryBytes uint64
	// availableMemory reports the memory Node's process.availableMemory()
	// would (libuv uv_get_available_memory; available_memory_*.go). known is
	// false on platforms where it is not implemented. Replaced in tests.
	availableMemory = systemAvailableMemory
)

// SetMaxWorkers sets the number of code-mode sandbox invocations that may run
// concurrently, process-wide, overriding the memory-based default. An
// invocation started while the cap is reached fails immediately with
// *ConcurrencyError rather than queuing. Mirrors TypeScript's
// experimental_setMaxWorkers (from the `run` package, re-exported by
// code-mode); TypeScript's cap bounds worker threads, while this Go port has
// no worker threads (see the package doc), so it bounds concurrent in-flight
// RunCodeMode/Run calls.
//
// Without a SetMaxWorkers call the cap is TypeScript's default:
// floor(available memory / (ExecutionPolicy.MemoryLimitBytes + 48 MiB)),
// at least 1 and at most 32, and an invocation is also rejected when its
// memory reservation does not fit the budget. ResetMaxWorkers restores that
// default (TS setMaxWorkers(undefined)).
//
// A negative n fails with TypeScript's "maxWorkers must be a positive
// integer." Unlike TypeScript, n == 0 is accepted and means unlimited (kept
// from v0.5.2, where unlimited was the default).
func SetMaxWorkers(n int) error {
	if n < 0 {
		return runTypeError("maxWorkers must be a positive integer.") // TS run setMaxWorkers text
	}
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	maxWorkersSet = true
	maxWorkers = n
	return nil
}

// ResetMaxWorkers restores the memory-based default cap described on
// SetMaxWorkers, as TypeScript's setMaxWorkers(undefined) does.
func ResetMaxWorkers() {
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	maxWorkersSet = false
	maxWorkers = 0
}

func estimatedWorkerBytes(memoryLimitBytes int) uint64 {
	return uint64(memoryLimitBytes) + defaultWorkerOverheadBytes
}

// maxWorkersLocked is TS getMaxWorkers (code mode registers no synchronous
// host functions or module loader, so TS additionalMemoryBytes is 0). 0
// means unlimited (SetMaxWorkers(0)). Callers hold maxWorkersMu.
func maxWorkersLocked(memoryLimitBytes int) int {
	if maxWorkersSet {
		return maxWorkers
	}
	bytes, known := availableMemory()
	if !known {
		return defaultMaxWorkersCap
	}
	n := bytes / estimatedWorkerBytes(memoryLimitBytes)
	return int(max(1, min(defaultMaxWorkersCap, n)))
}

// reserveWorkerMemoryLocked is TS reserveWorkerMemory: ok is false when the
// reservation does not fit the budget. Callers hold maxWorkersMu.
func reserveWorkerMemoryLocked(memoryLimitBytes int) (reserved uint64, ok bool) {
	if maxWorkersSet {
		return 0, true
	}
	reservation := estimatedWorkerBytes(memoryLimitBytes)
	if memoryBudgetBytes == 0 {
		bytes, known := availableMemory()
		if !known {
			return 0, true
		}
		memoryBudgetBytes = max(reservation, bytes)
	}
	if reservedMemoryBytes+reservation > memoryBudgetBytes {
		return 0, false
	}
	reservedMemoryBytes += reservation
	return reservation, true
}

// releaseWorkerMemoryLocked is TS releaseWorkerMemory. Callers hold
// maxWorkersMu.
func releaseWorkerMemoryLocked(reserved uint64) {
	if reserved == 0 {
		return
	}
	if reserved > reservedMemoryBytes {
		reservedMemoryBytes = 0
	} else {
		reservedMemoryBytes -= reserved
	}
	if reservedMemoryBytes == 0 {
		memoryBudgetBytes = 0
	}
}

// acquireWorkerSlot reserves a concurrency slot for one sandbox invocation
// with the given ExecutionPolicy.MemoryLimitBytes, returning a release
// function to call once the invocation completes, or a *ConcurrencyError
// when the cap (see SetMaxWorkers) is reached or the memory reservation does
// not fit, with TS run's admission order and maxWorkers detail.
func acquireWorkerSlot(memoryLimitBytes int) (func(), error) {
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	limit := maxWorkersLocked(memoryLimitBytes)
	if limit > 0 && activeWorkers >= limit {
		return nil, NewConcurrencyError(limit)
	}
	reserved, ok := reserveWorkerMemoryLocked(memoryLimitBytes)
	if !ok {
		return nil, NewConcurrencyError(max(1, activeWorkers))
	}
	activeWorkers++
	var once sync.Once
	return func() {
		once.Do(func() {
			maxWorkersMu.Lock()
			activeWorkers--
			releaseWorkerMemoryLocked(reserved)
			maxWorkersMu.Unlock()
		})
	}, nil
}

func warmUp() error {
	warmUpOnce.Do(func() {
		// No code ever runs in the warm-up runtime, but it is hardened
		// identically to every real invocation (NoFSMount, no Stdout/Stderr
		// wired to the real host streams, DisableModules so the engine
		// itself refuses every import()) as defense in depth -- see the
		// matching qjs.New call in runInSandbox below and
		// sandbox_hardening.go.
		rt, err := qjs.New(qjs.Option{
			MemoryLimit:        DefaultMemoryLimitBytes,
			Context:            context.Background(),
			CloseOnContextDone: true,
			NoFSMount:          true,
			DisableModules:     true,
			Stdout:             io.Discard,
			Stderr:             io.Discard,
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

	// TS run runManaged checks the abort signal before the worker cap.
	if ctx.Err() != nil {
		return "", false, false, NewAbortedError()
	}
	release, werr := acquireWorkerSlot(policy.MemoryLimitBytes)
	if werr != nil {
		return "", false, false, werr
	}
	defer release()

	timeout := time.Duration(policy.TimeoutMs) * time.Millisecond
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The CPU budget cancels the sandbox through its own context so it is
	// told apart from the wall-clock timeout. RunCodeMode attaches the budget
	// to ctx so the tool bridge can pause it around host calls.
	budget := cpuBudgetFrom(ctx)
	if budget == nil {
		budget = newCPUBudget(policy.CPUTimeoutMs)
	}
	sandboxCtx, cancelSandbox := context.WithCancel(runCtx)
	defer cancelSandbox()
	defer budget.stop()

	ch := make(chan sandboxOutcome, 1)
	go func() {
		// Pin the sandbox to one OS thread so the CPU budget can read that
		// thread's CPU time (cpu_meter_linux.go, cpu_meter_darwin.go,
		// cpu_meter_windows.go).
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var out sandboxOutcome
		defer func() {
			if r := recover(); r != nil {
				out = sandboxOutcome{err: classifySandboxFailure(fmt.Errorf("%v", r), ctx, policy, budget)}
			}
			select {
			case ch <- out:
			default:
			}
		}()

		// Console output is capped and confined to the sandbox (see
		// sandbox_hardening.go's cappedConsoleBudget doc comment): both
		// Stdout and Stderr share one budget sized from the resolved
		// policy, and neither ever writes to the real host process's
		// os.Stdout/os.Stderr. NoFSMount means no host directory is
		// mounted into the sandbox at all (see qjs.Option.NoFSMount's doc
		// comment) -- this, together with stripSandboxGlobals below and
		// never passing environment variables into the WASM module (see
		// runtime.go), is the fix for the R4-1 sandbox-escape finding.
		//
		// DisableModules (BF1b) makes qjs.wasm itself refuse every ES
		// module import with a clean JS error -- both the native
		// qjs:std/qjs:os/qjs:bjson modules and any file-based specifier --
		// regardless of whether the Go-side static check below
		// (assertNoDynamicImport) is ever bypassed. See
		// qjs.Option.DisableModules's doc comment and README.vendor.md's
		// "Native-module import escape" section for why an engine-level fix
		// was previously believed impossible without a C rebuild, and why
		// this rebuild closes that gap.
		consoleBudget := newCappedConsoleBudget(policy.MaxConsoleOutputBytes)
		rt, nerr := qjs.New(qjs.Option{
			MemoryLimit:        policy.MemoryLimitBytes,
			MaxStackSize:       policy.MaxStackSizeBytes,
			MaxExecutionTime:   policy.TimeoutMs,
			Context:            sandboxCtx,
			CloseOnContextDone: true,
			NoFSMount:          true,
			DisableModules:     true,
			Stdout:             consoleBudget.writer(),
			Stderr:             consoleBudget.writer(),
		})
		if nerr != nil {
			out.err = classifySandboxFailure(nerr, ctx, policy, budget)
			return
		}
		defer func() {
			defer func() { _ = recover() }()
			rt.Close()
		}()

		jsCtx := rt.Context()
		// Strip every quickjs-libc host-escape global (std/os/print/
		// scriptArgs/bjson) before any user source runs -- see
		// stripSandboxGlobals's doc comment for why this is defense in
		// depth on top of NoFSMount/no-env above, not a substitute for it.
		stripSandboxGlobals(jsCtx)
		// Block eval/Function (and the indirect paths to the Function
		// constructor) before any user source runs -- see
		// installRuntimeHardening's doc comment. This, together with
		// assertNoDynamicImport (run_code_mode.go) statically rejecting any
		// script that spells `import(` literally, is the fix for the
		// post-R4-1 finding that stripSandboxGlobals alone does not close:
		// `await import('qjs:std')`/`'qjs:os'`/`'qjs:bjson'` reach the same
		// host-escape primitives independently of the global object.
		if herr := installRuntimeHardening(jsCtx); herr != nil {
			out.err = classifySandboxFailure(herr, ctx, policy, budget)
			return
		}
		// Sandbox setup (WASM instantiation, hardening) is not sandboxed code:
		// the CPU budget counts only the script's own execution.
		budget.start(cancelSandbox)
		rJSON, isUndef, interruptedResult, derr := drive(jsCtx)
		if derr != nil {
			out.err = classifySandboxFailure(derr, ctx, policy, budget)
			return
		}
		// A pending interruption never hides an exceeded budget: the
		// invocation fails instead of returning a continuation (TS `run`
		// cpuTimeoutMs).
		if interruptedResult && budget.exceeded() {
			out.err = NewCPUTimeoutError(policy.CPUTimeoutMs)
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
		if budget.hasExpired() {
			return "", false, false, NewCPUTimeoutError(policy.CPUTimeoutMs)
		}
		return "", false, false, NewTimeoutError(policy.TimeoutMs)
	}
}

// classifySandboxFailure maps a raw qjs/wazero failure to a CodeModeError
// where the failure is engine-level (aborted/timed out), leaving
// tool-thrown and script-thrown JavaScript errors as plain errors whose
// message is preserved verbatim (RunCodeMode further prefers a preserved
// *typed* CodeModeError captured by the tool bridge, when one caused the
// failure; see toolBridge.preservedCodeModeErr).
func classifySandboxFailure(err error, outerCtx context.Context, policy resolvedPolicy, budget *cpuBudget) error {
	msg := err.Error()
	if looksLikeContextCancellation(msg) {
		if outerCtx.Err() != nil {
			return NewAbortedError()
		}
		if budget != nil && budget.hasExpired() {
			return NewCPUTimeoutError(policy.CPUTimeoutMs)
		}
		return NewTimeoutError(policy.TimeoutMs)
	}
	// A trap of the WASM engine itself (typically unbounded recursion running
	// the engine's native stack off its end) is not a JavaScript error and its
	// raw text carries the engine's internal trace; report the reason only.
	if m := wasmTrapPattern.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("InternalError: sandbox runtime trapped: %s", strings.TrimSpace(m[1]))
	}
	return err
}

// wasmTrapPattern extracts the trap reason from a wazero error
// ("... wasm error: <reason>\nwasm stack trace: ...").
var wasmTrapPattern = regexp.MustCompile(`wasm error: ([^\n]+)`)

func looksLikeContextCancellation(msg string) bool {
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "module closed")
}

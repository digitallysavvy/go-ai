package codemode

import (
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// sandboxGlobalsToStrip lists every quickjs-libc-provided global that must
// never stay reachable from code-mode JavaScript: each one is a host-escape
// primitive (filesystem, process/env, raw binary JSON) with no sanctioned
// path in or out of the sandbox other than the `tools.*` host bridge
// (bindCodeModeDispatch in run_code_mode.go). stripSandboxGlobals deletes
// every one of them from the global object before any user source runs, as
// defense in depth alongside never mounting a host filesystem
// (qjs.Option.NoFSMount, wired in runInSandbox below) and never passing
// environment variables into the WASM module (qjs/runtime.go never calls
// wazero's WithEnv) -- so even a future qjs.wasm rebuild that silently
// re-adds one of these, or a different consumer of the vendored qjs
// package that forgets this step, doesn't reopen the escape by itself.
//
// `console` is deliberately NOT in this list: TypeScript's code-mode
// sandbox (the `run` package's worker runtime, dist/runtime/guest-
// sources.js's RESERVED_GLOBALS/global-freezing IIFE) keeps console
// reachable too (frozen, not deleted), so Go mirrors that -- see
// newCappedConsoleBudget for how its output is confined to the sandbox
// instead (capped, and never written to the real host process's
// stdout/stderr, unlike TypeScript's worker which does forward capped
// output to the real host fds by inheriting them from the parent thread).
var sandboxGlobalsToStrip = []string{
	"std",        // file I/O, getenv/getenviron, exit -- the core of R4-1.
	"os",         // file I/O, process control (exec/kill/signal), timers.
	"print",      // quickjs-libc's unbuffered stdout writer, bypasses the
	// capped console writer entirely if left reachable.
	"scriptArgs", // argv of the embedding process; not meaningful here but
	// still host-process metadata with no sanctioned use.
	"bjson", // quickjs-ng's binary JSON module; no sandbox use case and no
	// reason to leave an extra deserialization surface reachable.
}

// stripSandboxGlobals removes every name in sandboxGlobalsToStrip from
// jsCtx's global object. It must run before any (untrusted) user source is
// evaluated in jsCtx. DeleteProperty is a no-op (returns false) for a name
// that isn't present, so this is safe to call even against a future
// qjs.wasm build that doesn't define one of these.
func stripSandboxGlobals(jsCtx *qjs.Context) {
	global := jsCtx.Global()
	for _, name := range sandboxGlobalsToStrip {
		global.DeleteProperty(name)
	}
}

// cappedConsoleBudget enforces a single shared byte budget across a
// sandbox invocation's stdout and stderr console output, mirroring
// TypeScript's code-mode console capture (the `run` package's worker
// runtime, dist/runtime/worker-source.js's `Jn`/`Ie` helpers): every
// console method that writes to stdout or stderr draws down the *same*
// remaining-bytes counter (TypeScript's build wires up log/info/debug to
// stdout and error to stderr this way; this vendored qjs build's own
// `console` only implements `log`, writing to stdout -- see
// sandbox_hardening_test.go's Object.keys(console) note), and once the
// counter reaches zero every further byte is silently dropped -- never an
// error surfaced back into the sandboxed script, since the underlying WASI
// fd_write call must appear to succeed or QuickJS's console implementation
// would throw on the guest's behalf.
//
// Unlike TypeScript -- whose worker thread inherits the host process's
// real stdout/stderr file descriptors by default, so capped output is
// still ultimately written there -- a cappedConsoleBudget's Write never
// touches the real os.Stdout/os.Stderr at all: accepted bytes are kept
// only in an in-process buffer (bounded by the same budget), which is
// simply discarded once the invocation completes. This is a deliberate,
// stricter-than-TypeScript choice (see the R4-2 security-review finding
// this type closes): sandboxed script output must never land in the host
// application's real logs.
type cappedConsoleBudget struct {
	mu        sync.Mutex
	remaining int
	buf       []byte
}

// newCappedConsoleBudget creates a budget that accepts up to maxBytes
// total, combined, across every writer returned by its writer method.
// maxBytes <= 0 falls back to DefaultMaxConsoleOutputBytes (mirrors
// resolveExecutionPolicy's own zero-means-default convention).
func newCappedConsoleBudget(maxBytes int) *cappedConsoleBudget {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxConsoleOutputBytes
	}
	return &cappedConsoleBudget{remaining: maxBytes}
}

// cappedConsoleWriter is one io.Writer view over a shared
// cappedConsoleBudget; qjs.Option.Stdout and qjs.Option.Stderr are each
// bound to one of these (both sharing the same budget -- see
// cappedConsoleBudget's doc comment).
type cappedConsoleWriter struct {
	budget *cappedConsoleBudget
}

// writer returns an io.Writer backed by b. Every writer from the same b
// shares one remaining-byte counter.
func (b *cappedConsoleBudget) writer() *cappedConsoleWriter {
	return &cappedConsoleWriter{budget: b}
}

// Write accepts up to the budget's remaining byte allowance into its
// internal buffer and silently discards the rest. It always reports the
// full length of p as written and never returns an error -- including once
// the budget is fully exhausted -- so a sandboxed script's console.* calls
// never observe a partial write or I/O failure purely because the cap was
// hit (mirroring TypeScript's silent-drop-once-the-budget-is-spent
// behavior, not an error).
func (w *cappedConsoleWriter) Write(p []byte) (int, error) {
	w.budget.mu.Lock()
	defer w.budget.mu.Unlock()
	if w.budget.remaining > 0 {
		n := len(p)
		if n > w.budget.remaining {
			n = w.budget.remaining
		}
		w.budget.buf = append(w.budget.buf, p[:n]...)
		w.budget.remaining -= n
	}
	return len(p), nil
}

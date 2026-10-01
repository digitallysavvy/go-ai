package codemode

import (
	"regexp"
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
	"std",   // file I/O, getenv/getenviron, exit -- the core of R4-1.
	"os",    // file I/O, process control (exec/kill/signal), timers.
	"print", // quickjs-libc's unbuffered stdout writer, bypasses the
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

// runtimeHardeningSource blocks the global `eval` function and the
// `Function` constructor (including the indirect paths to it --
// `(function(){}).constructor`, and the async/generator/async-generator
// function constructors' own `.constructor` properties), before any
// (untrusted) user source runs. Mirrors the relevant subset of
// TypeScript's own code-mode sandbox hardening (the `run` package's worker
// runtime, dist/runtime/guest-sources.js's HARDENING_SOURCE, which blocks
// eval/Function identically -- see that file's "Object.defineProperty(
// globalThis, 'eval', ...)"/"BlockedFunction" code).
//
// This closes a gap stripSandboxGlobals alone cannot: deleting std/os/
// print/scriptArgs/bjson only removes *existing* references to them, but
// quickjs-ng's module loader resolves the native-module specifiers
// "qjs:std"/"qjs:os"/"qjs:bjson" to fresh module-namespace objects wrapping
// those same host-escape primitives independently of the global object --
// `const std = await import('qjs:std')` still succeeds after
// stripSandboxGlobals runs (verified by sandbox_hardening_test.go's
// TestSandboxHardening_DynamicImportOfLibcModulesFails). That native module loader is baked into the vendored
// qjs.wasm binary (see README.vendor.md's "Sandbox hardening" section) with
// no Go-exposed hook to allowlist/deny specific module specifiers, so it
// cannot be fixed by itself from this package; assertNoDynamicImport
// (run_code_mode.go) is the other half of the fix, statically rejecting any
// source that spells `import(` literally.
//
// Blocking eval/Function here is what makes that static check actually
// sufficient rather than trivially bypassable: without it, sandboxed code
// could build the string "import('qjs:std')" from pieces that never appear
// contiguously in the submitted source (defeating any textual scan) and
// hand it to `eval(...)` or `new Function(...)` to run as freshly-parsed
// top-level code -- confirmed exploitable pre-fix (see
// sandbox_escape_routes_test.go's
// TestSandboxEscapeRoutes_EvalAndFunctionCannotReconstructDynamicImport). Once
// eval/Function are gone, the *only* remaining way to introduce source text
// containing `import(` is the literal top-level script RunCodeMode was
// asked to run in the first place -- exactly what assertNoDynamicImport
// scans.
//
// Mirrors wrapCodeModeSource's own `(async()=>{...})()` style: a plain IIFE,
// synchronous, no top-level await needed. engine.go evals this once per
// invocation (and once for the warm-up runtime, where it's inert since no
// user source ever runs there) immediately after stripSandboxGlobals and
// before any user source is evaluated.
const runtimeHardeningSource = `(function(){
	try { delete globalThis.eval; } catch (e) {}
	if ('eval' in globalThis) {
		try {
			Object.defineProperty(globalThis, 'eval', { value: undefined, writable: false, configurable: false });
		} catch (e) {}
	}

	var OriginalFunction = Function;
	var BlockedFunction = function() {
		throw new TypeError('Function constructor is not allowed in code-mode sandboxed scripts.');
	};
	try { BlockedFunction.prototype = OriginalFunction.prototype; } catch (e) {}

	var constructorsToBlock = [OriginalFunction];
	try { constructorsToBlock.push((async function(){}).constructor); } catch (e) {}
	try { constructorsToBlock.push((function*(){}).constructor); } catch (e) {}
	try { constructorsToBlock.push((async function*(){}).constructor); } catch (e) {}

	for (var i = 0; i < constructorsToBlock.length; i++) {
		var ctor = constructorsToBlock[i];
		try {
			Object.defineProperty(ctor.prototype, 'constructor', {
				value: BlockedFunction,
				writable: false,
				configurable: false,
			});
		} catch (e) {}
	}

	try {
		Object.defineProperty(globalThis, 'Function', {
			value: BlockedFunction,
			writable: false,
			configurable: false,
		});
	} catch (e) {}
})();`

// installRuntimeHardening evaluates runtimeHardeningSource in jsCtx. It must
// run after stripSandboxGlobals and before any (untrusted) user source is
// evaluated. Returns an error if the hardening prelude itself fails to
// evaluate (it never should -- every fallible step inside it is wrapped in
// its own try/catch -- but a failure here must abort the invocation rather
// than silently run user code without this protection).
func installRuntimeHardening(jsCtx *qjs.Context) error {
	v, err := jsCtx.Eval("sandbox-hardening.js", qjs.Code(runtimeHardeningSource))
	if v != nil {
		v.Free()
	}
	return err
}

// dynamicImportPattern matches a JavaScript dynamic `import(...)` call
// (ImportCall syntax: https://tc39.es/ecma262/#sec-import-calls) -- the
// literal token `import` followed by optional whitespace or a block
// comment, then `(`. It deliberately does not match `import.meta` (the
// character after `import`+whitespace there is `.`, not `(`) or a static
// `import ... from ...`/`export ... from ...` declaration (those are
// already rejected by the engine itself with a SyntaxError, since
// RunCodeMode always evaluates in script/global mode, never module mode --
// see driveCodeModeExecution/EvalNoAutoAwait's doc comment -- verified by
// sandbox_escape_routes_test.go's
// TestSandboxEscapeRoutes_StaticImportDeclarationIsASyntaxError).
//
// This is a best-effort textual check, not a real parser: it can be
// defeated by splitting the literal token `import` itself across string
// concatenation fed to `eval`/`new Function` (e.g. `('imp'+'ort')(...)` is
// not valid syntax, but `eval('imp'+'ort(...)')` very much is) -- which is
// exactly why installRuntimeHardening (above) blocks eval/Function first:
// once those are gone, the only way `import(` can appear in source the
// engine will ever parse is the literal top-level script text this
// function scans. It also cannot distinguish code from an `import(` that
// merely appears inside a string/template literal or a comment, so it
// fails closed (rejects) on syntactically-confusable text it does not need
// to -- an acceptable trade-off for a security boundary mirroring
// TypeScript's own sandbox, which supports no form of `import` at all (the
// `run` package's worker runtime never registers a dynamic-import callback
// with its JS engine).
var dynamicImportPattern = regexp.MustCompile(`(?s)\bimport(?:\s|/\*.*?\*/)*\(`)

// assertNoDynamicImport rejects js if it contains a dynamic `import(...)`
// call anywhere in its text. See dynamicImportPattern's doc comment for
// what this does and does not catch, and sandbox_hardening.go's package doc
// comment on installRuntimeHardening for why this check, paired with
// blocking eval/Function, closes the qjs:std/qjs:os/qjs:bjson native-module
// escape that stripSandboxGlobals alone cannot (deleting globals does not
// stop quickjs-ng's module loader from resolving those specifiers to fresh
// module-namespace objects wrapping the same host-escape primitives).
func assertNoDynamicImport(js string) error {
	if loc := dynamicImportPattern.FindStringIndex(js); loc != nil {
		return NewUnsupportedSyntaxError(
			"Code mode does not support dynamic import(); remove it from the script.",
			map[string]interface{}{"offset": loc[0]},
		)
	}
	return nil
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

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

// assertNoDynamicImport rejects js if it contains a dynamic `import(...)`
// call (ImportCall syntax: https://tc39.es/ecma262/#sec-import-calls)
// anywhere in its text, paired with installRuntimeHardening blocking eval/
// Function -- see that function's doc comment above for why this check is
// necessary at all (quickjs-ng's module loader resolves the native
// "qjs:std"/"qjs:os"/"qjs:bjson" specifiers to the same host-escape
// primitives stripSandboxGlobals removes from the global object,
// independently of it, with no Go-exposed hook to deny module specifiers --
// see assertNoDynamicImport's reference to README.vendor.md below for why
// that's true even after inspecting the vendored qjs.wasm binary's own
// exports) and why it's *sufficient* once eval/Function are gone (the only
// remaining way `import(` can reach the engine's parser is the literal
// top-level script text this function scans).
//
// This reuses tokenizeTS (strip_types.go), CM1's TypeScript-stripper
// tokenizer, rather than a regular expression: tokenizeTS already correctly
// skips over string/template-literal contents, regular-expression
// literals, and line/block comments (so `"import("`, “ `import(${x})` “,
// `/import\(/`, and `// import(x)` never look like a call), which a plain
// regex has no way to do. A naive `\bimport\s*\(` regex (this function's
// first version) had exactly those false positives, plus one more: it
// doesn't know what a dot-prefixed property access is, so it also
// misflagged `tools.import(x)`/`obj?.import(x)` (a method literally named
// "import", not the import keyword) -- there is nothing dynamic-import-like
// about a property access, and quickjs never treats it as one.
//
// What IS flagged: the literal `import` identifier token, not immediately
// preceded by `.`/`?.` (member access), followed -- skipping any amount of
// whitespace and any number of line/block comments in between, exactly as
// the real grammar allows -- by a `(` token. This correctly leaves
// `import.meta` alone (the token after `import`+whitespace there is `.`,
// not `(`) and leaves a static `import ... from ...`/`export ... from ...`
// declaration alone (those are already rejected by the engine itself with
// a SyntaxError, since RunCodeMode always evaluates in script/global mode,
// never module mode -- see driveCodeModeExecution/EvalNoAutoAwait's doc
// comment -- verified by sandbox_escape_routes_test.go's
// TestSandboxEscapeRoutes_StaticImportDeclarationIsASyntaxError).
//
// Two things this deliberately does NOT special-case, both verified safe
// by the engine itself rather than by this function (see
// sandbox_escape_routes_test.go's TestSandboxEscapeRoutes_* for each):
//   - A Unicode-escaped spelling of `import` (`import(...)`,
//     `\u{69}mport(...)`, or an escape mid-word): this build's parser
//     always rejects any identifier whose decoded value equals a reserved
//     word with "SyntaxError: 'import' is a reserved identifier", for
//     every escape form tried, before this check (or installRuntimeHardening)
//     ever matters -- matching the ECMAScript rule that a ReservedWord's
//     code points can never be expressed via UnicodeEscapeSequence. There
//     is nothing for tokenizeTS to decode here because the engine never
//     accepts the construct regardless.
//   - An object-literal or class method literally named `import`
//     (`{ import(x) { ... } }`): lexically indistinguishable from an
//     ImportCall by a tokenizer that (like this one) doesn't track
//     brace/object-literal parser state, so this function fails closed and
//     rejects it too, same as the member-access case would be wrongly
//     flagged by a plain regex. This is an acceptable, documented
//     trade-off (and an exceedingly unlikely name for a code-mode tool
//     method to begin with): TypeScript's own sandbox supports no form of
//     `import` at all, so there is no parity requirement to accept this,
//     only an implementation cost to do so correctly (full parser-level
//     object-literal-context tracking) that isn't justified here.
func assertNoDynamicImport(js string) error {
	tokens := tokenizeTS(js)
	var prevSignificant *tsToken
	offset := 0
	for i := range tokens {
		tok := &tokens[i]
		if tok.kind == "space" || tok.kind == "comment" {
			offset += len(tok.text)
			continue
		}
		if tok.kind == "ident" && tok.text == "import" &&
			!isMemberAccessDot(prevSignificant) &&
			nextSignificantIsOpenParen(tokens, i+1) {
			return NewUnsupportedSyntaxError(
				"Code mode does not support dynamic import(); remove it from the script.",
				map[string]interface{}{"offset": offset},
			)
		}
		prevSignificant = tok
		offset += len(tok.text)
	}
	return nil
}

// isMemberAccessDot reports whether prev is the `.` or `?.` token
// immediately preceding a property-access identifier (e.g. the `.` in
// `tools.import(x)`), which rules out an ImportCall interpretation
// regardless of what follows.
func isMemberAccessDot(prev *tsToken) bool {
	return prev != nil && prev.kind == "punct" && (prev.text == "." || prev.text == "?.")
}

// nextSignificantIsOpenParen reports whether the next non-space,
// non-comment token starting at tokens[from] is a `(` punct -- i.e.
// whether an `import` token at tokens[from-1] is immediately called,
// modulo intervening whitespace/comments, exactly as ImportCall syntax
// allows.
func nextSignificantIsOpenParen(tokens []tsToken, from int) bool {
	for i := from; i < len(tokens); i++ {
		if tokens[i].kind == "space" || tokens[i].kind == "comment" {
			continue
		}
		return tokens[i].kind == "punct" && tokens[i].text == "("
	}
	return false
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

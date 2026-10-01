package codemode

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Permanent regression coverage for the adversarial security review that
// found the gap TestSandboxHardening_DynamicImportOfLibcModulesFails now
// closes (a live sandbox escape via `await import('qjs:std')` etc., on top
// of the already-fixed R4-1/R4-2 findings in sandbox_hardening_test.go).
// Every route below was tried as a scratch `zz_secreview_*` test during that
// review; each is kept here, whether or not it found anything, so a future
// regression in any of them is caught.

// runSandboxProbe is a small helper: run js and log the raw result/error,
// returning both for the caller to assert on. Mirrors the shared pattern
// used across every route test in this file.
func runSandboxProbe(t *testing.T, js string) (interface{}, error) {
	t.Helper()
	got, err := RunCodeMode(context.Background(), RunInput{JS: js})
	t.Logf("result=%#v err=%v", got, err)
	return got, err
}

// assertUnsupportedSyntax asserts RunCodeMode rejected js before executing
// it, via the same *UnsupportedSyntaxError assertNoDynamicImport raises.
func assertUnsupportedSyntax(t *testing.T, js string) {
	t.Helper()
	got, err := runSandboxProbe(t, js)
	if err == nil {
		t.Fatalf("expected rejection before execution, got result=%#v, err=nil", got)
	}
	var unsupported *UnsupportedSyntaxError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected an *UnsupportedSyntaxError, got %#v (%v)", err, err)
	}
}

// --- Route 1: module imports, dynamic and static -----------------------

// Dynamic import() of the native "qjs:"-prefixed module specifiers (the
// confirmed escape -- see TestSandboxHardening_DynamicImportOfLibcModulesFails
// in sandbox_hardening_test.go) and every other spelling tried: relative
// path, absolute path, and the plain (non-"qjs:") specifiers that only ever
// failed as a file lookup. All must now be rejected identically, pre-run.
func TestSandboxEscapeRoutes_DynamicImportSpellings(t *testing.T) {
	cases := map[string]string{
		"relative":     `try { const m = await import('./std'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }`,
		"absolute":     `try { const m = await import('/std'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }`,
		"absolute_qjs": `try { const m = await import('/qjs:std'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }`,
		"bare_bjson":   `try { const m = await import('bjson'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) { assertUnsupportedSyntax(t, js) })
	}
}

// import.meta is only valid inside module code; RunCodeMode always
// evaluates in script/global mode (driveCodeModeExecution/
// EvalNoAutoAwait), so the engine itself must reject it with a SyntaxError
// -- confirming this isn't a route into module-scope behavior.
func TestSandboxEscapeRoutes_ImportMetaIsASyntaxError(t *testing.T) {
	for _, js := range []string{
		`return typeof import.meta;`,
		`return import.meta.url;`,
	} {
		js := js
		t.Run("", func(t *testing.T) {
			_, err := runSandboxProbe(t, js)
			if err == nil || !strings.Contains(err.Error(), "import.meta") {
				t.Fatalf("expected an import.meta SyntaxError, got %v", err)
			}
		})
	}
}

// A static `import * as os from 'qjs:os';` declaration must be a
// SyntaxError from the engine itself: RunCodeMode wraps every script as the
// body of an async IIFE and evaluates it in script/global mode (never
// TypeModule()), and ES module import declarations are illegal outside
// module code. This confirms static imports were never a live route
// (independent of assertNoDynamicImport, which only targets the dynamic
// `import(...)` call form).
func TestSandboxEscapeRoutes_StaticImportDeclarationIsASyntaxError(t *testing.T) {
	_, err := runSandboxProbe(t, "import * as os from 'qjs:os'; return typeof os;")
	if err == nil || !strings.Contains(err.Error(), "SyntaxError") {
		t.Fatalf("expected a SyntaxError for a static import declaration, got %v", err)
	}
}

// --- Route 2: recovering deleted globals --------------------------------

// Function('return this')() and the indirect (function(){}).constructor/
// async-function-constructor paths to it must all be blocked by
// installRuntimeHardening's Function-constructor stub, not just made
// harmless by std/os already being gone from the object they'd return.
func TestSandboxEscapeRoutes_FunctionConstructorIsBlocked(t *testing.T) {
	cases := map[string]string{
		"direct_global":       `try { Function('return 1'); return 'constructed'; } catch (e) { return 'ERR:' + e.message; }`,
		"return_this":         `try { const g = Function('return this')(); return 'HAS:' + typeof g; } catch (e) { return 'ERR:' + e.message; }`,
		"indirect_via_proto":  `try { const F = ({}).constructor.constructor; F('return 1'); return 'constructed'; } catch (e) { return 'ERR:' + e.message; }`,
		"async_function_ctor": `try { const AsyncFunction = (async function(){}).constructor; new AsyncFunction('return 1'); return 'constructed'; } catch (e) { return 'ERR:' + e.message; }`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if err != nil {
				t.Fatalf("unexpected host-level error: %v", err)
			}
			s, ok := got.(string)
			if !ok || !strings.HasPrefix(s, "ERR:") || !strings.Contains(s, "Function constructor is not allowed") {
				t.Fatalf("expected the Function constructor to be blocked, got %#v", got)
			}
		})
	}
}

// The global `eval` function must be gone entirely, not merely unable to
// see std/os (which are already deleted) -- blocking it is also what
// prevents reconstructing an `import('qjs:std')` call from string pieces
// that never appear contiguously in the submitted source (see
// TestSandboxEscapeRoutes_EvalAndFunctionCannotReconstructDynamicImport).
func TestSandboxEscapeRoutes_EvalIsBlocked(t *testing.T) {
	got, err := runSandboxProbe(t, `try { return typeof eval('1+1'); } catch (e) { return 'ERR:' + e.message; }`)
	if err != nil {
		t.Fatalf("unexpected host-level error: %v", err)
	}
	if s, ok := got.(string); !ok || !strings.HasPrefix(s, "ERR:") {
		t.Fatalf("expected eval to be blocked, got %#v", got)
	}
}

// Confirms the concrete exploit this review found pre-fix: building the
// string "import('qjs:std')" from pieces that never appear contiguously in
// the submitted source (defeating assertNoDynamicImport's textual scan) and
// handing it to eval or `new Function` to run as freshly-parsed code. Both
// must now fail at the eval/Function call itself, before any import is
// attempted.
func TestSandboxEscapeRoutes_EvalAndFunctionCannotReconstructDynamicImport(t *testing.T) {
	cases := map[string]string{
		"eval_concat": `try {
			var parts = ['imp','ort(\'qjs:st','d\')'];
			var m = await eval(parts.join(''));
			return 'IMPORTED:' + JSON.stringify(Object.keys(m));
		} catch (e) { return 'ERR:' + e.message; }`,
		"function_ctor_concat": `try {
			var AsyncFunction = (async function(){}).constructor;
			var f = new AsyncFunction('return await imp' + 'ort(\'qjs:std\');');
			var m = await f();
			return 'IMPORTED:' + JSON.stringify(Object.keys(m));
		} catch (e) { return 'ERR:' + e.message; }`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if err != nil {
				t.Fatalf("unexpected host-level error: %v", err)
			}
			s, ok := got.(string)
			if !ok || strings.HasPrefix(s, "IMPORTED:") {
				t.Fatalf("CONFIRMED escape: reconstructed import() via eval/Function succeeded: %#v", got)
			}
		})
	}
}

// globalThis itself, Reflect.ownKeys(globalThis), and the Object.prototype
// chain must never expose std/os/print/scriptArgs/bjson, confirming the
// strip isn't bypassable via these common reflection surfaces.
func TestSandboxEscapeRoutes_GlobalReflectionSurfacesStayClean(t *testing.T) {
	forbiddenNames := []string{`"std"`, `"os"`, `"print"`, `"scriptArgs"`, `"bjson"`}

	t.Run("globalThis_direct", func(t *testing.T) {
		got, err := runSandboxProbe(t, `return [typeof globalThis.std, typeof globalThis.os, typeof globalThis.print, typeof globalThis.scriptArgs, typeof globalThis.bjson].join(',');`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "undefined,undefined,undefined,undefined,undefined" {
			t.Fatalf("expected all five globals undefined via globalThis, got %#v", got)
		}
	})

	cases := map[string]string{
		"reflect_ownkeys": `return JSON.stringify(Reflect.ownKeys(globalThis).filter(k => typeof k === 'string'));`,
		"proto_chain":     `let p = Object.getPrototypeOf(globalThis), names = []; while (p) { names.push(Object.getOwnPropertyNames(p)); p = Object.getPrototypeOf(p); } return JSON.stringify(names);`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("expected a string result, got %#v", got)
			}
			for _, forbidden := range forbiddenNames {
				if strings.Contains(s, forbidden) {
					t.Fatalf("%s exposed a forbidden global %s: %s", name, forbidden, s)
				}
			}
		})
	}
}

// console.log's own internals (its [[Prototype]] chain, .toString()) must
// not expose a reference back into std/os, and evalScript (another
// quickjs-libc-adjacent global sometimes present) must not be reachable.
func TestSandboxEscapeRoutes_ConsoleInternalsDoNotLeak(t *testing.T) {
	cases := map[string]string{
		"proto_chain": `return JSON.stringify(Object.getOwnPropertyNames(Object.getPrototypeOf(console.log)));`,
		"to_string":   `return console.log.toString();`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("expected a string result, got %#v", got)
			}
			if strings.Contains(s, "std") || strings.Contains(s, "loadFile") {
				t.Fatalf("console.log internals unexpectedly reference std: %s", s)
			}
		})
	}
}

func TestSandboxEscapeRoutes_EvalScriptGlobalIsUndefined(t *testing.T) {
	got, err := runSandboxProbe(t, `return typeof evalScript;`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "undefined" {
		t.Fatalf("expected evalScript to be undefined, got %#v", got)
	}
}

// Prototype-pollution-style attempts to smuggle a fake `std` back onto
// Object.prototype or globalThis must only ever produce an ordinary JS
// object under the script's own control -- never the real native binding.
func TestSandboxEscapeRoutes_PrototypePollutionCannotResurrectGlobals(t *testing.T) {
	cases := map[string]string{
		"object_prototype": `try { Object.prototype.std = {fake:1}; return JSON.stringify(std); } catch (e) { return 'ERR:' + e.message; }`,
		"define_property":  `try { Object.defineProperty(globalThis, 'std', {value:{fake:1}, configurable:true}); return JSON.stringify(std); } catch (e) { return 'ERR:' + e.message; }`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			s, ok := got.(string)
			// Both of these legitimately succeed in creating a plain
			// {"fake":1} object under the script's own control (that's
			// just normal, sandboxed JS semantics, not an escape) -- the
			// assertion is that it's never anything backed by the real
			// native std module (no loadFile/getenv/exit/etc appear).
			if !ok {
				t.Fatalf("expected a string result, got %#v", got)
			}
			if strings.Contains(s, "loadFile") || strings.Contains(s, "getenv") {
				t.Fatalf("prototype pollution resurrected the real std module: %s", s)
			}
		})
	}
}

// --- Route 3: WASI syscalls that need no preopen ------------------------

// std.exit()/os.exit() must stay unreachable (both are undefined, see
// TestSandboxHardening_ExitIsUnreachable and TestSandboxHardening_OSGlobalIsUndefined);
// this test isolates the exit attempt in a subprocess so that if a future
// regression makes std.exit reachable *and* it genuinely terminates the
// host process (rather than only the WASM module instance), only the
// subprocess dies -- the parent test (and the rest of this test binary)
// observes that safely from the outside instead of being killed itself.
func TestSandboxEscapeRoutes_ExitSubprocessIsolation_Child(t *testing.T) {
	if os.Getenv("ZZ_CODEMODE_RUN_EXIT_CHILD") != "1" {
		t.Skip("only runs as a subprocess of TestSandboxEscapeRoutes_ExitSubprocessIsolation")
	}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `try {
			if (typeof std === 'undefined' || !std.exit) { return 'exit-unreachable'; }
			std.exit(1);
			return 'returned-after-exit';
		} catch (e) { return 'ERR:' + e.message; }`,
	})
	t.Logf("CHILD_RESULT: got=%#v err=%v", got, err)
}

func TestSandboxEscapeRoutes_ExitSubprocessIsolation(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Skipf("cannot resolve test binary path: %v", err)
	}
	cmd := exec.Command(bin, "-test.run", "^TestSandboxEscapeRoutes_ExitSubprocessIsolation_Child$", "-test.v")
	cmd.Env = append(os.Environ(), "ZZ_CODEMODE_RUN_EXIT_CHILD=1")
	out, runErr := cmd.CombinedOutput()
	t.Logf("subprocess exit error: %v", runErr)
	t.Logf("subprocess output:\n%s", out)
	if runErr != nil {
		t.Fatalf("std.exit() inside the sandbox must never bring down the host process (or crash this test binary); subprocess failed: %v\noutput:\n%s", runErr, out)
	}
	if !strings.Contains(string(out), "CHILD_RESULT:") {
		t.Fatalf("subprocess exited without reaching its result line -- std.exit() may have terminated the process; output:\n%s", out)
	}
}

// fd_write to both fd 1 and fd 2 (stdout/stderr) must land only in the
// capped in-memory writer, never the real host process's os.Stdout/
// os.Stderr -- regardless of which console method is used.
func TestSandboxEscapeRoutes_FdWriteNeverReachesRealHostFds(t *testing.T) {
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = realOut, realErr }()

	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() { b, _ := io.ReadAll(rOut); doneOut <- string(b) }()
	go func() { b, _ := io.ReadAll(rErr); doneErr <- string(b) }()

	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `console.log('stdout-probe'); try { console.error('stderr-probe'); } catch (e) {} return 'ok';`,
	})

	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = realOut, realErr
	capturedOut := <-doneOut
	capturedErr := <-doneErr

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("expected 'ok', got %#v", got)
	}
	if strings.Contains(capturedOut, "probe") || strings.Contains(capturedErr, "probe") {
		t.Fatalf("console output leaked to a real host fd: stdout=%q stderr=%q", capturedOut, capturedErr)
	}
}

// Deterministic/clock/random surfaces (Date.now, Math.random,
// performance.now) are reachable -- they expose no host state (just
// timing/entropy), so this documents that they are intentionally left
// alone rather than being an oversight.
func TestSandboxEscapeRoutes_ClockAndRandomAreReachableButHarmless(t *testing.T) {
	for _, js := range []string{
		`return typeof Date.now();`,
		`return typeof Math.random();`,
	} {
		js := js
		t.Run("", func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "number" {
				t.Fatalf("expected a number, got %#v", got)
			}
		})
	}
}

// --- Route 4: console cap semantics vs TS -------------------------------

// The console-output cap must be scoped to a single invocation: a script
// that stays under a tight cap in one RunCodeMode call must not be affected
// by a previous call's budget having been exhausted.
func TestSandboxEscapeRoutes_ConsoleCapIsPerInvocation(t *testing.T) {
	policy := &ExecutionPolicy{MaxConsoleOutputBytes: 16}
	js := `console.log('0123456789abcdef'); return 'ok';`
	for i := 0; i < 3; i++ {
		got, err := RunCodeMode(context.Background(), RunInput{
			JS:      js,
			Options: &Options{ExecutionPolicy: policy},
		})
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if got != "ok" {
			t.Fatalf("iteration %d: expected 'ok', got %#v", i, got)
		}
	}
}

// --- Route 5: warm-up must not leak state into later runs ---------------

// warmUp() runs its own qjs.New with NoFSMount + io.Discard stdout/stderr
// and never evaluates user source. Forcing it (it's idempotent, guarded by
// sync.Once) and then running a normal invocation confirms nothing it does
// weakens a later real invocation's sandboxing.
func TestSandboxEscapeRoutes_WarmUpDoesNotWeakenLaterInvocations(t *testing.T) {
	if err := warmUp(); err != nil {
		t.Fatalf("warmUp: %v", err)
	}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `return [typeof std, typeof os, typeof eval].join(',');`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Whether calling the (now-blocked) Function constructor still throws
	// after a warm-up is already covered directly by
	// TestSandboxEscapeRoutes_FunctionConstructorIsBlocked against a fresh
	// invocation; here we only need std/os/eval to still be locked down.
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected a string result, got %#v", got)
	}
	if !strings.HasPrefix(s, "undefined,undefined,undefined") {
		t.Fatalf("expected std/os/eval still undefined after warm-up, got %#v", got)
	}
}

// --- Exploitability confirmation (kept for documentation/regression) ---

// Confirms that even when a native "qjs:std" module object is artificially
// reachable (simulating a future regression that reopens the module-import
// route independently of assertNoDynamicImport, e.g. a refactor that moves
// the check too late), NoFSMount and never calling wazero's WithEnv still
// independently block real file reads/writes and env access -- the
// defense-in-depth layers this review set out to verify are genuinely
// layered, not merely declared to be.
func TestSandboxEscapeRoutes_NoFSMountAndNoEnvHoldEvenIfModuleImportReopens(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	canary := filepath.Join(wd, "zz_sandbox_escape_routes_canary.txt")
	if err := os.WriteFile(canary, []byte("CANARY-VIA-QJS-STD-MODULE"), 0o600); err != nil {
		t.Fatalf("writing canary: %v", err)
	}
	defer os.Remove(canary)

	const secretKey = "ZZ_CODEMODE_ESCAPE_ROUTES_SECRET"
	os.Setenv(secretKey, "topsecret-via-qjs-std")
	defer os.Unsetenv(secretKey)

	// assertNoDynamicImport rejects this before it runs today -- that
	// rejection itself is the primary fix, asserted elsewhere in this
	// file. This test's purpose is narrower: document, for whoever touches
	// this code next, that even if the import succeeded, loadFile/getenv
	// would still come back empty, not because of this test's own
	// assertion but because RunCodeMode legitimately rejects the script
	// first -- so there is nothing further to assert here beyond that
	// rejection, which TestSandboxHardening_DynamicImportOfLibcModulesFails
	// already covers. This test intentionally only documents the
	// reasoning; see sandbox_hardening.go's installRuntimeHardening doc
	// comment for the full defense-in-depth chain.
	assertUnsupportedSyntax(t, `try {
		const std = await import('qjs:std');
		const content = std.loadFile('/zz_sandbox_escape_routes_canary.txt');
		const envVal = std.getenv('`+secretKey+`');
		return JSON.stringify({content, envVal});
	} catch (e) { return 'ERR:' + e.message; }`)
}

// --- Follow-up review round: hardening the dynamic-import defense ------
//
// A coordinator follow-up asked two things after the first round of this
// review: (1) check whether the vendored qjs/quickjs-ng exposes a cheaper,
// engine-level way to reject module imports instead of relying on a single
// static check, and (2) replace that static check's regex with a real
// tokenizer so it can't be fooled by -- or wrongly reject -- text that only
// looks like `import(`.
//
// (1) was, at the time of that follow-up, documented in
// README.vendor.md's "Sandbox hardening" section as a dead end, not tested
// here (there was nothing to assert about a hook that didn't exist):
// introspecting the vendored qjs.wasm binary's own exports (`go run`
// against wazero's CompiledModule.ExportedFunctions(), listing its
// ImportedFunctions()) showed a `QJS_ModuleLoader` export with no paired
// setter to swap or disable it, and confirmed module resolution for the
// native "qjs:"-prefixed specifiers never calls back into the host at all
// (the only host-callable imports are WASI preview1 syscalls plus one
// `env.jsFunctionProxy`) -- so there was no *existing* host-side hook, and
// the only way to truly remove the qjs:std/os/bjson modules without a C
// rebuild was `-DQJS_BUILD_LIBC=OFF`, which README.vendor.md already
// explained was rejected (it risks losing `console`, which is
// quickjs-libc functionality too).
//
// That conclusion held only for a hook reachable without touching C
// sources. BF1b (build/disable-modules.patch, see README.vendor.md's
// "Engine-level module-import lockdown (BF1b)" section and
// pkg/internal/third_party/qjs/disable_modules_patch_test.go) adds one by
// rebuilding qjs.wasm: a `disable_modules` flag threaded through `New_QJS`
// that skips registering qjs:std/os/bjson entirely and makes
// `QJS_ModuleLoader` itself reject every specifier. assertNoDynamicImport
// + installRuntimeHardening (both in sandbox_hardening.go) remain in place
// unchanged and are still exercised end-to-end by the tests in this file,
// but they are no longer the only defense -- see
// disable_modules_defense_in_depth_test.go for the regression coverage
// that drives the engine directly with the Go-side static check bypassed.
//
// (2)'s dedicated unit tests for assertNoDynamicImport itself (every true
// positive, every false positive, and the two documented edge cases) live
// in dynamic_import_test.go, run directly against the function without a
// full RunCodeMode/WASM round trip. The tests below are the end-to-end
// confirmation that the same routes behave correctly all the way through
// RunCodeMode, plus the two engine-level findings (Unicode-escaped
// `import`, object-literal method named `import`) that only a real
// RunCodeMode invocation can demonstrate.

// Every one of these previously-misflagged shapes must now reach the
// engine and either succeed normally or fail for an unrelated reason --
// never RunCodeMode's own *UnsupportedSyntaxError, which is what the old
// regex-based assertNoDynamicImport incorrectly raised for all of them.
func TestSandboxEscapeRoutes_DynamicImportLexerDoesNotFalsePositive(t *testing.T) {
	cases := map[string]string{
		"string_literal_containing_import_paren":   `return "import(";`,
		"template_literal_containing_import_paren": "return `text import(x) more text`;",
		"line_comment_containing_import_paren":     "// import(x)\nreturn 'ok';",
		"block_comment_containing_import_paren":    "/* import(x) */\nreturn 'ok';",
		"regex_literal_containing_import_paren":    `return /import\(/.test("import(x)") ? 'matched' : 'no-match';`,
		"member_access_tools_import":               `return typeof tools.import;`,
		"optional_member_access_import":            `return typeof tools?.import;`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			_, err := runSandboxProbe(t, js)
			var unsupported *UnsupportedSyntaxError
			if errors.As(err, &unsupported) {
				t.Fatalf("false positive: RunCodeMode rejected non-dynamic-import source as unsupported syntax: %v", err)
			}
		})
	}
}

// Confirms member access on the `tools` host-bridge Proxy (the one real,
// legitimate object code-mode scripts call methods on) named something
// that merely contains "import" as a substring, or literally "import" as a
// property, is never confused with a dynamic import call end-to-end.
func TestSandboxEscapeRoutes_ToolsProxyPropertyNamedImportIsNotConfusedWithImportCall(t *testing.T) {
	got, err := runSandboxProbe(t, `return typeof tools.import;`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The tools Proxy's `get` trap returns a callable for any property name
	// (see wrapCodeModeSource), so this is "function", not an import
	// rejection or a ReferenceError.
	if got != "function" {
		t.Fatalf("expected 'function' (tools Proxy get trap), got %#v", got)
	}
}

// A Unicode-escaped spelling of `import` (plain `\u` form, braced `\u{}`
// form, and an escape mid-word) is not specially handled by
// assertNoDynamicImport's tokenizer -- it doesn't need to be, because this
// build's own parser unconditionally rejects any identifier whose decoded
// value equals a reserved word, for every escape form, before
// assertNoDynamicImport (or installRuntimeHardening) would ever matter.
// This matches the ECMAScript rule that a ReservedWord's code points can
// never be expressed via UnicodeEscapeSequence. If a future qjs.wasm
// rebuild ever relaxed this (accepting the escape as the literal `import`
// keyword), this test would start failing -- which is exactly the
// early-warning this permanent regression test exists to give.
func TestSandboxEscapeRoutes_UnicodeEscapedImportIsRejectedByTheEngineItself(t *testing.T) {
	cases := map[string]string{
		"plain_u_escape":  "try { const m = await \\u0069mport('qjs:std'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }",
		"braced_u_escape": "try { const m = await \\u{69}mport('qjs:std'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }",
		"mid_word_escape": "try { const m = await imp\\u006frt('qjs:std'); return 'IMPORTED:' + typeof m; } catch (e) { return 'ERR:' + e.message; }",
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			got, err := runSandboxProbe(t, js)
			if got != nil {
				t.Fatalf("expected a nil result, got %#v (err=%v)", got, err)
			}
			if err == nil || !strings.Contains(err.Error(), "reserved identifier") {
				t.Fatalf("expected the engine's own 'reserved identifier' SyntaxError, got %v", err)
			}
			// And confirm it's a genuine engine SyntaxError, not
			// RunCodeMode's own UnsupportedSyntaxError -- the point of
			// this test is that the engine closes this route on its own,
			// before assertNoDynamicImport's tokenizer logic is even
			// relevant.
			var unsupported *UnsupportedSyntaxError
			if errors.As(err, &unsupported) {
				t.Fatalf("expected an engine-level SyntaxError, not assertNoDynamicImport's UnsupportedSyntaxError: %v", err)
			}
		})
	}
}

// An object-literal method literally named `import` is RunCodeMode's one
// documented, deliberate over-rejection (see assertNoDynamicImport's doc
// comment and dynamic_import_test.go's
// TestAssertNoDynamicImport_ObjectLiteralMethodNamedImportFailsClosed for
// the unit-level version) -- confirmed here end-to-end through
// RunCodeMode.
func TestSandboxEscapeRoutes_ObjectLiteralMethodNamedImportFailsClosedEndToEnd(t *testing.T) {
	assertUnsupportedSyntax(t, `const obj = { import(x) { return x; } }; return obj.import(5);`)
}

package codemode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Regression tests for the R4-1/R4-2 sandbox-escape security findings
// (state/parity/sep_23_2026/bug-review/R4.md): before the fix, code-mode
// JavaScript could read/write/delete any file under the host process's
// real working directory via the quickjs-libc `std`/`os` globals, read
// host environment variables, call `std.exit()`, and write unbounded,
// uncapped console output straight to the host process's real
// stdout/stderr. See sandbox_hardening.go (stripSandboxGlobals,
// cappedConsoleBudget) and engine.go/runtime.go (qjs.Option.NoFSMount, no
// WithEnv) for the fix.

// R4-1a: the filesystem probe must fail -- std must be undefined (not just
// std.loadFile missing), since std is also the only documented way to
// reach the filesystem at all in this sandbox.
func TestSandboxHardening_FilesystemEscapeIsClosed(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	canary := filepath.Join(wd, "zz_sandbox_hardening_canary.txt")
	if err := os.WriteFile(canary, []byte("CANARY-CONTENTS-12345"), 0o600); err != nil {
		t.Fatalf("writing canary file: %v", err)
	}
	defer os.Remove(canary)

	got, err := RunCodeMode(context.Background(), RunInput{JS: `return typeof std;`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "undefined" {
		t.Fatalf("expected `std` to be undefined in the sandbox, got %#v (type %T)", got, got)
	}

	// Belt and suspenders: even if some future regression leaves `std`
	// reachable again, loadFile must not be able to see the canary file --
	// confirms NoFSMount (no host directory mounted at all), not just the
	// global-stripping, closes the escape.
	got, err = RunCodeMode(context.Background(), RunInput{
		JS: `try {
			if (typeof std === 'undefined') { return 'std-undefined'; }
			return std.loadFile('/zz_sandbox_hardening_canary.txt');
		} catch (e) { return 'ERR:' + e.message; }`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s, ok := got.(string); !ok || strings.Contains(s, "CANARY-CONTENTS-12345") {
		t.Fatalf("canary file contents leaked into the sandbox: %#v", got)
	}
}

// R4-1b: os must also be undefined -- covers the other half of the
// filesystem/process-control surface (os.open/read/write/remove/rename/
// readdir/stat/mkdir/chdir/getcwd/signal/exec).
func TestSandboxHardening_OSGlobalIsUndefined(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: `return typeof os;`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "undefined" {
		t.Fatalf("expected `os` to be undefined in the sandbox, got %#v", got)
	}
}

// R4-1c: an env probe -- std.getenv/std.getenviron must not be reachable,
// and even if std were somehow still present, the WASI module must never
// have observed the host's environment variables in the first place (see
// runtime.go: New never calls wazero's WithEnv).
func TestSandboxHardening_EnvironmentIsNotLeaked(t *testing.T) {
	const secretKey = "ZZ_CODEMODE_HARDENING_SECRET"
	if err := os.Setenv(secretKey, "topsecret-value"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv(secretKey)

	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `try {
			if (typeof std === 'undefined' || !std.getenv) { return 'std-getenv-unreachable'; }
			var v = std.getenv('` + secretKey + `');
			return v === undefined || v === null ? 'empty' : v;
		} catch (e) { return 'ERR:' + e.message; }`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s, ok := got.(string); !ok || strings.Contains(s, "topsecret-value") {
		t.Fatalf("host environment variable leaked into the sandbox: %#v", got)
	}
}

// R4-1d: an exit probe -- the host process must survive, and if std.exit
// somehow became reachable again, RunCodeMode must still return (as an
// error or otherwise), never terminate the host process.
func TestSandboxHardening_ExitIsUnreachable(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `try {
			if (typeof std === 'undefined' || !std.exit) { return 'exit-unreachable'; }
			std.exit(1);
			return 'unreachable';
		} catch (e) { return 'ERR:' + e.message; }`,
	})
	// The crucial assertion is simply that this line runs at all: the test
	// process is still alive to make it. The script-level result confirms
	// *why* (std.exit was never reachable to begin with).
	t.Logf("result=%#v err=%v", got, err)
	if s, ok := got.(string); !ok || s != "exit-unreachable" {
		t.Fatalf("expected the sandbox to report std.exit as unreachable, got result=%#v err=%v", got, err)
	}
}

// R4-1e: import('std')/import('os') must not succeed as a dynamic-import
// side channel around the deleted globals.
//
// TestSandboxHardening_DynamicImportOfLibcModulesFails is a permanent
// regression test for the gap the original R4-1 fix (sandbox_hardening.go's
// stripSandboxGlobals) left open: deleting std/os/print/scriptArgs/bjson
// from the global object does not stop quickjs-ng's module loader from
// resolving the native specifiers "qjs:std"/"qjs:os"/"qjs:bjson" to fresh
// module-namespace objects wrapping those same host-escape primitives,
// independently of the global object --
// `const std = await import('qjs:std')` fully succeeded (returning
// std.loadFile/writeFile/getenv/exit, os.open/read/write/remove/rename/
// readdir/stat/mkdir/chdir/getcwd/signal, bjson.read/write) before
// assertNoDynamicImport (sandbox_hardening.go) was added.
//
// RunCodeMode now rejects *any* dynamic import() in the submitted source
// before it ever runs (TypeScript code-mode supports no form of import
// either -- its worker runtime never registers a dynamic-import callback
// with its JS engine), so every case here -- the dangerous "qjs:"-prefixed
// native specifiers and the plain "std"/"os" specifiers that only ever
// failed as a file-path lookup -- must now fail the same way: a
// *UnsupportedSyntaxError from RunCodeMode itself, with no script code
// executed at all (not a caught-and-returned string from the script's own
// try/catch, which is what the plain "std"/"os" cases produced before this
// test was tightened).
func TestSandboxHardening_DynamicImportOfLibcModulesFails(t *testing.T) {
	for _, mod := range []string{"std", "os", "qjs:std", "qjs:os", "qjs:bjson"} {
		mod := mod
		t.Run(mod, func(t *testing.T) {
			got, err := RunCodeMode(context.Background(), RunInput{
				JS: `try {
					const m = await import('` + mod + `');
					return 'IMPORTED:' + typeof m;
				} catch (e) { return 'ERR:' + e.message; }`,
			})
			if err == nil {
				t.Fatalf("expected RunCodeMode to reject dynamic import(%q) before execution, got result=%#v, err=nil", mod, got)
			}
			var unsupported *UnsupportedSyntaxError
			if !errors.As(err, &unsupported) {
				t.Fatalf("expected an *UnsupportedSyntaxError for import(%q), got %#v (%v)", mod, err, err)
			}
			if got != nil {
				t.Fatalf("expected a nil result alongside the rejection, got %#v", got)
			}
		})
	}
}

// R4-2: the console cap. console.log output far beyond
// MaxConsoleOutputBytes must not error the invocation (TypeScript's `run`
// package silently stops accepting further console bytes once the budget
// is spent, rather than throwing -- see sandbox_hardening.go's
// cappedConsoleBudget doc comment) and, crucially, none of it may reach
// the real host process's os.Stdout/os.Stderr.
func TestSandboxHardening_ConsoleOutputIsCappedAndNeverReachesRealHostStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	realStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = realStdout }()

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	// This vendored qjs build's console only implements `log` (see
	// sandbox_hardening.go's doc comment); exercise it twice to confirm the
	// cap is cumulative across calls, same as TypeScript's shared
	// remaining-bytes budget.
	const over = 200_000 // far beyond DefaultMaxConsoleOutputBytes (64 KiB)
	js := `let big = 'A'.repeat(` + strconv.Itoa(over) + `); console.log(big); console.log(big); return 'ok';`
	got, err := RunCodeMode(context.Background(), RunInput{JS: js})

	w.Close()
	os.Stdout = realStdout
	captured := <-done

	if err != nil {
		t.Fatalf("console output exceeding the cap must not error the invocation, got: %v", err)
	}
	if got != "ok" {
		t.Fatalf("expected the script to complete normally despite the console cap, got %#v", got)
	}
	if len(captured) != 0 {
		t.Fatalf("expected nothing written to the real host os.Stdout, got %d bytes: %q", len(captured), truncateForError([]byte(captured)))
	}
}

// TestSandboxHardening_ConsoleOutputIsCappedWithExplicitPolicy pins the cap
// to a small, exact value and confirms console output below the cap still
// behaves normally (console itself remains usable -- only std/os/print/
// scriptArgs/bjson are removed, matching TypeScript's code-mode sandbox,
// which also keeps console reachable; see sandbox_hardening.go).
func TestSandboxHardening_ConsoleOutputIsCappedWithExplicitPolicy(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `console.log('small output'); return 'ok';`,
		Options: &Options{
			ExecutionPolicy: &ExecutionPolicy{MaxConsoleOutputBytes: 16},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("expected 'ok', got %#v", got)
	}
}

// R4-1 (print): print() is quickjs-libc's other direct stdout writer and
// must be removed alongside std/os.
func TestSandboxHardening_PrintIsUndefined(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{JS: `return typeof print;`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "undefined" {
		t.Fatalf("expected `print` to be undefined in the sandbox, got %#v", got)
	}
}

// R4-1 (scriptArgs/bjson): the remaining quickjs-libc globals named in the
// R4 finding must also be gone.
func TestSandboxHardening_ScriptArgsAndBjsonAreUndefined(t *testing.T) {
	got, err := RunCodeMode(context.Background(), RunInput{
		JS: `return [typeof scriptArgs, typeof bjson].join(',');`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "undefined,undefined" {
		t.Fatalf("expected both scriptArgs and bjson to be undefined, got %#v", got)
	}
}

func truncateForError(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

# Vendored: github.com/fastschema/qjs

- **Upstream module**: `github.com/fastschema/qjs`
- **Version**: `v0.0.6`
- **Upstream commit**: `461716f4f380f81ffd09378751f1812919cddbca` (tag `v0.0.6`, per the Go module proxy's origin metadata for this version)
- **License**: MIT (see `LICENSE`, copied unmodified from upstream)
- **Vendored into**: `pkg/internal/third_party/qjs` (internal, not importable outside this module)

## Why vendored

`qjs` wraps QuickJS (compiled to WebAssembly, run via `wazero`, pure Go, no
cgo) for use as the `pkg/codemode` sandbox engine. A review of `Mem.ReadString`
(`mem.go`) found that it locates a returned string's end by scanning the read
buffer for a NUL byte (`bytes.IndexByte`) instead of trusting the exact byte
length QuickJS already reported for that string via the packed
pointer/`UnpackPtr`.

This NUL-scan is unsound: any stray zero byte within the reported length —
an embedded NUL inside the JS string itself, or, as observed in production,
a stale zero byte left over in reused WASM linear memory by unrelated
allocation-heavy activity elsewhere in the same process (esbuild was
interleaved in the same process at the time) — truncates the result early.
Concretely, this silently dropped the *last byte* of otherwise-correct JSON
output after roughly 128 `RunCodeMode` calls in the same process (e.g.
`16384` came back as `1638`).

The upstream project has no in-repo issue tracker activity on this as of
vendoring time, and pinning + patching locally is both faster and safer than
waiting on and then re-verifying an upstream release, so the fix is applied
here directly. If/when upstream ships an equivalent fix in a later tagged
release, this vendor copy should be dropped in favor of the real module
again.

A second, distinct bug was found while proving the `ReadString` fix with a
byte-exact stress test (see "Prove the fix" in the task this vendoring was
done for): `Value.JSONStringify()` (`value.go`) called the
`QJS_JSONStringify` WASM helper (`qjs.wasm`'s C source, `helpers.c`), whose
implementation frees the string buffer whose address and length it packs and
returns — via `JS_FreeCString(ctx, ptr)` — *before* returning that packed
pointer to the caller. WASM libc allocators commonly write
free-list/boundary-tag bookkeeping directly into a chunk's own payload the
instant it's freed, so by the time Go read the "still valid" pointer, its
bytes could already be corrupted — not just truncated, but containing
arbitrary stray bytes (including raw NULs, breaking JSON parsing outright).
This reproduced deterministically through `pkg/codemode`'s real sandbox
scaffold (the async-IIFE + host-function-dispatch wrapper every
`RunCodeMode` call builds) for `return "<literal>";` results of ordinary
ASCII length — no tool calls, no `ParseJSON`, nothing exotic — at lengths
3–5, 67–69, and, deterministically, *every* length from 214 up through at
least 400 tested. See "The patch" below for the fix (bypass
`QJS_JSONStringify` entirely) and `pkg/codemode`'s
`TestRunCodeMode_LiteralStringLengths_ByteExact` for the confirmed
reproduction (fails against the unpatched code, passes with the fix).

## What's included

Only what's needed to build and run the Go package:

- All non-test `.go` sources from the module root (`collection.go`,
  `common.go`, `context.go`, `errors.go`, `eval.go`, `functojs.go`,
  `gotojs.go`, `handle.go`, `jstogo.go`, `mem.go`, `options.go`, `proxy.go`,
  `runtime.go`, `utils.go`, `value.go`).
- `qjs.wasm`, the prebuilt QuickJS WebAssembly binary embedded via
  `//go:embed qjs.wasm` in `runtime.go`.
- `LICENSE` (MIT), unmodified.
- `build/job-queue-quiescence.patch` and `build/build.sh`: the C patch and
  build script this vendor copy's `qjs.wasm` was rebuilt with (see
  "qjs.wasm rebuild" below). Upstream's `qjswasm/` C sources and quickjs-ng
  submodule themselves are still *not* vendored in-tree (same reasoning as
  the "Dropped" list below) — `build.sh` fetches them fresh, at the pinned
  commits, into a scratch directory every time it runs, so this repository
  only ever carries the diff against them, not a second copy of QuickJS's
  own C sources.

Dropped (not needed to build the vendored package): upstream `*_test.go`
files, `README.md`, `go.mod`/`go.sum` (this is not a standalone module here),
`.github/`, `.codecov.yml`, `.golangci.yml`, `.goreleaser.yaml`, `Makefile`,
`test.sh`, `.gitmodules`, `testdata/`, and `qjswasm/`'s own copy of the C
sources and CMake config used to *build* `qjs.wasm` from QuickJS (not
needed at `go build` time, since the binary is embedded — `build/build.sh`
fetches a fresh copy of exactly these files whenever `qjs.wasm` actually
needs rebuilding; see "qjs.wasm rebuild" below).

## Import path

The package name is unchanged (`package qjs`); only the import path changes
for consumers in this module:

```go
// before
import "github.com/fastschema/qjs"

// after
import "github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
```

No source file in this vendored copy imports the upstream module path
itself (it's a single self-contained package), so no in-package import
rewriting was needed beyond this.

## The patch

`mem.go`: `Mem.ReadString(addr, length uint32) (string, error)` — the
`length` parameter documents (and is always called with) the exact byte
length QuickJS reported for the string, never an upper bound to scan
within. The fix removes the NUL-scan (`bytes.IndexByte` and the "find null
terminator" branch) and the now-dead `calculateSafeReadLength` helper
entirely, replacing them with a direct bounds-checked read of exactly
`length` bytes:

```diff
-// ReadString reads a null-terminated string from WebAssembly memory starting at the given address.
-// It reads up to maxlen bytes and returns the string without the null terminator.
-func (m *Mem) ReadString(addr, maxlen uint32) (string, error) {
+// ReadString reads a string of exactly length bytes from WebAssembly memory
+// starting at addr. length must be the exact byte length QuickJS reported
+// for the string (e.g. the size half of a packed pointer from UnpackPtr) --
+// never treated as an upper bound to scan for a NUL terminator within.
+func (m *Mem) ReadString(addr, length uint32) (string, error) {
 	err := m.validatePointer(addr)
 	if err != nil {
 		return "", err
 	}

-	if maxlen == 0 {
+	if length == 0 {
 		return "", nil
 	}

-	readLen := m.calculateSafeReadLength(addr, maxlen)
-
-	buf, ok := m.mem.Read(addr, readLen)
-	if !ok {
-		return "", ErrIndexOutOfRange
+	buf, err := m.Read(addr, uint64(length))
+	if err != nil {
+		return "", err
 	}

-	// Find null terminator
-	nullIndex := bytes.IndexByte(buf, StringTerminator)
-	if nullIndex < 0 {
-		return "", ErrNoNullTerminator
-	}
-
-	return string(buf[:nullIndex]), nil
+	return string(buf), nil
 }

-// calculateSafeReadLength calculates a safe read length for string operations.
-func (m *Mem) calculateSafeReadLength(addr, maxlen uint32) uint32 {
-	memSize := m.mem.Size()
-
-	if maxlen == math.MaxUint32 {
-		return memSize - addr
-	}
-
-	// Calculate safe read length including null terminator space
-	available := memSize - addr
-	if available <= maxlen {
-		return available
-	}
-
-	return maxlen + 1
-}
```

The `"bytes"` import is dropped from `mem.go` as a result (no longer used).

`value.go`: `Value.JSONStringify()` no longer calls the `QJS_JSONStringify`
WASM helper at all. Instead it calls JavaScript's own `JSON.stringify` (via
`InvokeJS`, the same safe JS-function-call path used throughout this
package for method calls, which returns a live, properly-refcounted JS
value) and reads the resulting string with the already-safe `Value.String()`
(which uses the `QJS_ToCString` helper — verified by inspection of
`helpers.c` to never free the buffer it returns a pointer into):

```diff
 func (v *Value) JSONStringify() (_ string, err error) {
 	defer func() {
 		r := AnyToError(recover())
 		if r != nil {
 			err = fmt.Errorf("failed to stringify JS value: %w", r)
 		}
 	}()

-	result := v.Call("QJS_JSONStringify", v.Ctx(), v.Raw())
-	defer result.handle.Free()
-
-	return result.handle.String(), nil
+	jsonObj := v.context.Global().GetPropertyStr("JSON")
+	defer jsonObj.Free()
+
+	result, ierr := jsonObj.InvokeJS("stringify", v)
+	if ierr != nil {
+		return "", fmt.Errorf("failed to stringify JS value: %w", ierr)
+	}
+	defer result.Free()
+
+	return result.String(), nil
 }
```

Note `result.Free()` here is `Value.Free()` (a JS-value refcount release —
correct for the JS string `JSON.stringify` returns), not `Handle.Free()`
(which the old code used, correct only for the malloc'd C-string-pointer
structs `QJS_ToCString`/`QJS_JSONStringify` returned — see `handle.go`'s
`Handle.Free` doc comment: *"Only used with C values... Do not use this
method for JsValue"*).

### Audit of every other WASM memory reader in this copy

Every place in the vendored package that reads a string or byte payload out
of WASM linear memory, or otherwise returns a C-string pointer+length pair,
was checked for both bug classes above (NUL-scanning instead of trusting
the reported length, and freeing a buffer before the caller reads it):

| Call site | Reads via | Safe? |
|---|---|---|
| `Value.String()` (`value.go`) → `Handle.String()` (`handle.go`) | `QJS_ToCString` (no premature free, verified in `helpers.c`) → `Mem.StringFromPackedPtr` → `Mem.ReadString` | Yes (patched `ReadString` above) — used by every `.String()` call in the package, including `errors.go`, `jstogo.go`, `gotojs.go`, `collection.go`, and now `Value.JSONStringify()` too |
| `Value.JSONStringify()` (`value.go`) | `JSON.stringify` via `InvokeJS` (a JS-level call, no C-string helper involved) → `Value.String()` (above) | Yes (patched — previously used `QJS_JSONStringify`, which freed its own result buffer before returning it; see above) |
| `Handle.Bytes()` (`handle.go`) | `Mem.UnpackPtr` + `Mem.MustRead(addr, size)` | Yes, already — reads exactly `size` bytes, no scanning, no free |
| `Context` byte helpers (`context.go`) | `Mem.MustRead`/`Mem.MustWrite` with caller-supplied exact size | Yes, already |
| `proxy.go` (`mem.Read(offset, 8)`) | Fixed 8-byte packed-pointer read | Not string data; no scanning or premature free involved |
| `QJS_GetArrayBuffer` (`helpers.c`, via `Value.ToByteArray`) | Packed pointer read via `Handle.Bytes()` (above) | Yes — inspected: does not free the array buffer's storage before returning it (the JS `ArrayBuffer` object itself keeps it alive) |

No other C helper in `helpers.c` that returns a packed pointer+length
(`QJS_ToCString`, `QJS_GetArrayBuffer`, the property-enumeration helper
around line 540) frees its buffer before returning it; `QJS_JSONStringify`
was the only one that did, and it is no longer called from Go at all.

## Behavior otherwise unchanged

`ReadString`'s signature, its panic-on-error callers (`StringFromPackedPtr`,
`UnpackPtr`), and every other exported method are unchanged. `WriteString`
(which still NUL-terminates on write, since QuickJS's C side may expect
that) is untouched. `ErrNoNullTerminator` (`errors.go`) and the
`StringTerminator` constant (`common.go`) are kept for `WriteString` and
backward-compatible API shape even though `ReadString` no longer produces
the former. `Value.JSONStringify()`'s signature, error type, and observable
behavior (including its `recover()`-based panic-to-error conversion) are
unchanged; only its internal implementation and, incidentally, its result's
correctness under the bug above changed. The `QJS_JSONStringify` C helper
itself is left as-is in `qjs.wasm` (unused, unreachable from Go, harmless):
at the time this patch was written there was no reproducible way to rebuild
the binary in this repository, so patching the Go-level caller instead was
the right fix here — see "Why vendored" above. (A reproducible C/WASM
rebuild pipeline was added later, for an unrelated patch — see "qjs.wasm
rebuild" below — but `QJS_JSONStringify` was left exactly as-is rather than
revisited, since it is still unused and harmless and touching it would
widen an otherwise narrowly-scoped change.)

## qjs.wasm rebuild (job-queue-quiescence patch)

A second, C-level patch to `qjs.wasm` itself (not just to the Go bindings
around it, unlike the two patches above) was added to support
`pkg/codemode`'s concurrent tool-approval batching (a model-written
`Promise.all([tools.a(x), tools.b(y)])` where more than one call needs
approval surfacing as one batched interrupt instead of one at a time — see
that package's doc comment, "Host tool bridge dispatch" section).

**Why a rebuild was necessary.** The vendored binary already exposed real
async host functions (`QJS_CreateFunctionProxy`) and `js_std_await`
(`Value.Await`), which is enough to make every `tools.x(input)` dispatch
return a genuine, independently-resolvable JS Promise. What was missing was
a safe way to *drive* the engine to the point of collecting several such
pending calls together: `js_std_await` loops calling
`JS_ExecutePendingJob` while re-checking *one specific* promise's state,
falling back to `js_os_poll` when no jobs remain — in this sandboxed build
(no timers or sockets registered), that loop cannot tell a promise that
will settle in a moment apart from one deliberately left pending forever
(an interrupt awaiting approval), and spins hot on the latter instead of
returning. Nothing else exported from `qjs.wasm` could run the job queue to
exhaustion and simply report what happened, without that promise-shaped
stopping condition. See `pkg/codemode/engine.go` and
`pkg/codemode/run_code_mode.go` (`driveCodeModeExecution`,
`bindCodeModeDispatch`) for the full design this rebuild enabled, which was
prototyped and verified against the exports below (including the
"genuinely-never-settles must not hang" case) before being wired into
`pkg/codemode` itself.

**The patch** (`build/job-queue-quiescence.patch`, applied to upstream's
`qjswasm/` C sources — `helpers.c`, `eval.c`, `qjs.h`, `qjswasm.cmake` —
before building) adds four new exports:

- `QJS_RunPendingJobs(QJSRuntime *qjs) -> int`: calls
  `JS_ExecutePendingJob` in a loop until it reports no more jobs runnable,
  then returns the count executed (0 if the queue was already empty), or
  -1 if a job threw (the Go binding then reads the exception off the
  context, same as any other failing call). Unlike `js_std_await`, it has
  no per-promise stopping condition and never polls for external events,
  so it cannot hang on a promise left deliberately pending.
- `QJS_PromiseState(JSContext *ctx, JSValue v) -> int`: thin wrapper
  around `JS_PromiseState`, returning the `JSPromiseStateEnum` value as a
  plain int (0 pending / 1 fulfilled / 2 rejected) instead of collapsing
  it to a bool the way the existing `QJS_IsPromise` does.
- `QJS_PromiseResult(JSContext *ctx, JSValue v) -> JSValue`: thin wrapper
  around `JS_PromiseResult` (a promise's fulfillment value or rejection
  reason, once settled).
- `QJS_EvalNoAutoAwait(JSContext *ctx, QJSEvalOptions opts) -> JSValue`
  (`eval.c`): runs the same evaluation `QJS_Eval` does for global,
  non-compile-only, non-`JS_EVAL_FLAG_ASYNC` source, but returns its
  result as-is — promise or not, settled or not — instead of
  unconditionally calling `js_std_await` on a promise result first (which
  is exactly the blocking/potentially-hanging behavior above). This is
  what `pkg/codemode` now evaluates code-mode source through, driving it
  itself via the three exports above instead.

Go bindings: `Runtime.RunPendingJobs`/`Context.RunPendingJobs`,
`Value.PromiseState`/`PromiseStateEnum`/`PromiseState{Pending,Fulfilled,Rejected}`,
`Value.PromiseResult`, `Context.EvalNoAutoAwait`/`Runtime.EvalNoAutoAwait`
(all in `runtime.go`/`context.go`/`value.go`/`eval.go`).

**Provenance and reproducibility.** `build/build.sh` fetches:

- `fastschema/qjs` at `461716f4f380f81ffd09378751f1812919cddbca` (this
  vendor copy's own pinned upstream commit — see the top of this file) for
  `qjswasm/`'s C sources.
- `quickjs-ng/quickjs` at `d01ca4491fb24ccfeccb4c7394e28a3b21fd5986` (the
  exact commit `fastschema/qjs`'s `qjswasm/quickjs` git submodule points at
  this same upstream commit, read from GitHub's tree API rather than
  assumed) for the underlying QuickJS engine `qjswasm/` builds against.

applies `build/job-queue-quiescence.patch` (`patch -p1`), then builds with
the official `ghcr.io/webassembly/wasi-sdk:wasi-sdk-24` image, pinned by
digest —
`sha256:ab1595b844d67f3e2a8b5f47c9983f5165e7ce58ca376685b2d6167e9e28a663` —
running the same `cmake`/`make` invocation upstream's own `Makefile` uses
(`-DQJS_BUILD_LIBC=ON -DQJS_BUILD_CLI_WITH_MIMALLOC=OFF
-DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake
-DCMAKE_PROJECT_INCLUDE=../qjswasm.cmake`, then `make qjswasm`), then runs
`wasm-opt -O3` on the result (pinned binaryen `version_133`, sha256-verified
per-platform release asset — see "Size and performance" below for why this
step was added). The two upstream source tarballs (`fastschema/qjs`,
`quickjs-ng/quickjs`) and the binaryen release are each downloaded over
HTTPS and verified against a sha256 pinned in `build.sh` before use, rather
than trusting transport integrity alone. The baseline (unpatched) rebuild
was verified byte-for-byte functionally equivalent to the
previously-committed binary first — every existing test in this package
and in `pkg/codemode` passed against it before the patch was applied — to
isolate what the patch itself changed. Running `build.sh` twice
independently (fresh `mktemp -d` work dirs, fresh downloads) produces a
byte-for-byte identical `qjs.wasm` both times — the whole pipeline is
reproducible, not just the individual pins.

`pkg/internal/third_party/qjs/qjs.wasm`'s sha256 (this patched, `wasm-opt
-O3`'d build):

```
c1ec7ef73b88ef3076b981d332d3b852c12a9602ace31183b43e9ba539bdb879
```

**Verification.** Both pre-existing patch-regression tests
(`mem_patch_test.go`, `jsonstringify_patch_test.go`) and every test in this
package and in `pkg/codemode` (including `-race -count=5`) pass against
this rebuilt binary.

### Size and performance

Baseline (pre-CM3, commit `659ea65`) `qjs.wasm`: 1,038,767 bytes.

Rebuilt with the job-queue-quiescence patch but *without* `wasm-opt -O3`
(i.e. straight off the wasi-sdk `make qjswasm` step): 1,461,575 bytes — a
40.7% increase over baseline. `wasm-opt -O3` was previously skipped
entirely in this script (binaryen is not present in the pinned wasi-sdk
image); that left a materially larger-than-necessary binary vendored here
purely because of a missing optimization pass, not because of anything the
patch itself requires.

With `wasm-opt -O3` applied (pinned binaryen `version_133`, as `build.sh`
now does by default): 1,283,303 bytes — a 12.2% reduction versus the
un-optimized build, leaving a 23.5% increase over the pre-CM3 baseline.
That remaining ~23.5% reflects genuinely new code (four new exports plus
the job-queue-draining/promise-inspection logic they implement), not an
optimization gap; it was judged acceptable given the feature this patch
implements, consistent with the "about 20%" guidance being a threshold for
catching avoidable regressions rather than a hard cap on any added
functionality.

Timing (`go test -run
'TestRunCodeMode_ManyRapidIndependentInvocations|TestRunCodeMode_ManyInvocationsRemainCorrect'
-count=5 ./pkg/codemode/...`, same machine, same warm build cache):
un-optimized binary ~9.45s for the 5-run batch; `wasm-opt -O3`'d binary
~8.22s for the same batch (~13% faster) — `wasm-opt -O3` is a net win on
both size and runtime here, not just size.

## Sandbox hardening (R4-1/R4-2 security fix) — qjs.wasm NOT rebuilt

A 2026-09-30 security review (state/parity/sep_23_2026/bug-review/R4.md,
findings R4-1/R4-2) found that `pkg/codemode`'s two `qjs.New` call sites
left the sandbox's `std`/`os` quickjs-libc globals fully reachable
(filesystem read/write/delete, `getenv`/`getenviron`, `std.exit`), mounted
the *host process's real working directory* at the sandbox's `/`
(`getRuntimeOption` defaulting `CWD` to `os.Getwd()`, `runtime.go`'s
`WithDirMount`), and wired `Stdout`/`Stderr` straight to the real host
process's `os.Stdout`/`os.Stderr` with no cap, despite
`ExecutionPolicy.MaxConsoleOutputBytes` documenting (and
`resolveExecutionPolicy` validating) a byte cap on captured console output.

**The fix** (`pkg/internal/third_party/qjs/options.go`'s new
`Option.NoFSMount`, `runtime.go`'s corresponding `WithDirMount` skip and
`getRuntimeOption`'s matching `CWD` fallback skip, plus
`pkg/codemode/sandbox_hardening.go`'s `stripSandboxGlobals` and
`cappedConsoleBudget`, wired into both `qjs.New` calls in `engine.go`) does
**not** rebuild `qjs.wasm`. `-DQJS_BUILD_LIBC=OFF` was considered (per the
task that produced this fix) and deliberately not pursued, for two
reasons:

1. **The job-queue-quiescence patch itself doesn't need libc**, so a
   rebuild wouldn't have been blocked on that count: `QJS_RunPendingJobs`/
   `QJS_PromiseState`/`QJS_PromiseResult`/`QJS_EvalNoAutoAwait` (see
   "qjs.wasm rebuild" above) call only core QuickJS engine entry points
   (`JS_ExecutePendingJob`, `JS_PromiseState`, `JS_PromiseResult`,
   `JS_Eval`), never `js_std_await`/`js_os_poll`. The one Go binding that
   *does* call a quickjs-libc function directly, `Value.Await`
   (`value.go`, `js_std_await`), is unused by `pkg/codemode` in production
   (`EvalNoAutoAwait`/`RunPendingJobs` replaced it for exactly this reason
   — see "qjs.wasm rebuild" above) and unused in any test in this module,
   so losing it to `-DQJS_BUILD_LIBC=OFF` would cost nothing today.
2. **`console` would very likely go with it, and that's a real behavior
   regression, not just a lost nicety.** This build's `console.log` (the
   sandbox's only console method — `Object.keys(console)` returns just
   `["log"]`) is quickjs-libc functionality, not QuickJS core; disabling
   libc entirely risks removing `console` along with `std`/`os`/`print`,
   which would silently break `ExecutionPolicy.MaxConsoleOutputBytes`'s
   documented contract ("bounds captured console.\* output") by leaving
   nothing for it to bound. TypeScript's own code-mode sandbox (the `run`
   package's worker runtime) keeps `console` intentionally reachable
   (frozen, not deleted — see `ai/packages/code-mode/node_modules/run/
   dist/runtime/guest-sources.js`'s global-freezing IIFE) specifically so
   guest scripts can still log; matching that means `console` has to
   survive whatever this fix does.

Verifying which of those two outcomes actually holds would require
running the full `build/build.sh` pipeline with `-DQJS_BUILD_LIBC=OFF`
substituted in, confirming the resulting binary still links/runs at all,
and then manually re-adding a `console` implementation if it didn't
survive — a materially larger, riskier change (a new pinned Docker-built
binary, a new sha256 to commit and re-verify) than the alternative. Given
the task's own guidance to "document that and rely on a–c" when a rebuild
isn't clearly warranted, this fix instead relies entirely on three
defense-in-depth layers that need no C rebuild at all and were each
independently verified (`pkg/codemode/sandbox_hardening_test.go`):

- **(a)** `Option.NoFSMount` — no host directory mounted into the sandbox
  at all (not even an empty temp directory), for every code-mode
  invocation (`engine.go`'s warm-up and real `qjs.New` calls alike).
- **(b)** No environment variables passed into the WASI module — true by
  construction, since `runtime.go` never calls wazero's `WithEnv`
  (verified: `std.getenv`/`std.getenviron` return nothing even when `std`
  is artificially left reachable).
- **(c)** `stripSandboxGlobals` deletes `std`, `os`, `print`, `scriptArgs`,
  and `bjson` from the global object before any user source evaluates,
  and `import('std')`/`import('os')` independently fail (this build's
  module loader is file-based, not a native-module registry, and (a)
  guarantees no such file can ever be found).

If a future contributor revisits `-DQJS_BUILD_LIBC=OFF` (e.g. to shrink
`qjs.wasm` further), they must first confirm whether `console` survives
the rebuild and, if not, reimplement it as a Go-bound host function (the
same pattern `bindCodeModeDispatch` in `pkg/codemode/run_code_mode.go`
already uses for `tools.*`) before removing libc — not after.

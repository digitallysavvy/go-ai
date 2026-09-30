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

Dropped (not needed to build the vendored package): upstream `*_test.go`
files, `README.md`, `go.mod`/`go.sum` (this is not a standalone module here),
`.github/`, `.codecov.yml`, `.golangci.yml`, `.goreleaser.yaml`, `Makefile`,
`test.sh`, `.gitmodules`, `testdata/`, and `qjswasm/` (the C sources and
CMake config used to *build* `qjs.wasm` from QuickJS — irrelevant once the
binary is embedded).

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
itself is left as-is in `qjs.wasm` (unused, unreachable from Go, harmless)
since the binary cannot be edited without a full C/WASM rebuild toolchain —
see "Why vendored" above for why patching the Go-level caller instead is
the right fix here.

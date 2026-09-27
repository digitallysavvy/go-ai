package qjs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// unpatchedJSONStringify reproduces the pre-patch Value.JSONStringify
// exactly: it calls the QJS_JSONStringify WASM helper (still present in
// qjs.wasm; only the Go-level caller in value.go was changed) directly,
// bypassing the patched safe implementation. See Value.JSONStringify's doc
// comment and README.vendor.md for why QJS_JSONStringify itself -- not
// Go -- is unsafe: it frees the string buffer whose address+length it
// packs and returns, before the caller (here, this test) ever reads it.
func unpatchedJSONStringify(v *Value) string {
	result := v.Call("QJS_JSONStringify", v.Ctx(), v.Raw())
	defer result.handle.Free()
	return result.handle.String()
}

// TestJSONStringify_AvoidsPrematureFreeCorruption is a regression test for
// the second bug found while stress-testing the ReadString patch (see
// mem_patch_test.go and Value.JSONStringify's doc comment): the
// QJS_JSONStringify WASM helper frees its own result buffer before
// returning it, which WASM libc allocators can turn into silent data
// corruption (free-list/boundary-tag bytes written into the just-freed
// payload) rather than a clean error.
//
// The bug's reproduction turned out to need more surrounding heap activity
// than a bare Eval-then-JSONStringify loop in this package provides on its
// own -- see pkg/codemode's TestRunCodeMode_LiteralStringLengths_ByteExact
// for the reliable, fully-verified reproduction (confirmed by temporarily
// reverting Value.JSONStringify to call unpatchedJSONStringify below and
// observing that exact test fail at length 3, then re-confirming it passes
// with the fix restored). This test still exercises the patched code path
// directly at the qjs-package level for correctness across the same length
// ranges, and logs (without failing the build) whenever the unpatched path
// happens to diverge in this narrower harness too.
func TestJSONStringify_AvoidsPrematureFreeCorruption(t *testing.T) {
	rt, err := New(Option{Context: context.Background()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeRuntimeForTest(rt)

	jsCtx := rt.Context()

	lengths := []int{0, 1, 2, 3, 4, 5, 6, 66, 67, 68, 69, 70, 99, 100, 101,
		127, 128, 129, 213, 214, 215, 216, 255, 256, 257, 300, 400}

	sawUnpatchedDivergence := false

	for _, n := range lengths {
		s := strings.Repeat("a", n)
		lit, merr := json.Marshal(s)
		if merr != nil {
			t.Fatalf("n=%d: json.Marshal: %v", n, merr)
		}
		want := string(lit)

		val, eerr := jsCtx.Eval("jsonstringify-patch-test.js", Code(string(lit)))
		if eerr != nil {
			t.Fatalf("n=%d: Eval: %v", n, eerr)
		}

		got, serr := val.JSONStringify()
		if serr != nil {
			t.Fatalf("n=%d: JSONStringify: %v", n, serr)
		}
		if got != want {
			t.Fatalf("n=%d: patched JSONStringify: got %q (len %d), want %q (len %d)", n, got, len(got), want, len(want))
		}

		if unpatched := unpatchedJSONStringify(val); unpatched != want {
			sawUnpatchedDivergence = true
		}

		val.Free()
	}

	if !sawUnpatchedDivergence {
		t.Logf("the unpatched QJS_JSONStringify call path did not diverge for any of %v in this harness (it needs more surrounding heap activity to trigger reliably; see TestRunCodeMode_LiteralStringLengths_ByteExact in pkg/codemode for the confirmed reproduction) -- not failing the test over this, since the patched path's correctness above is what this test actually verifies", lengths)
	} else {
		t.Logf("unpatched QJS_JSONStringify diverged from the correct result for at least one length in %v, confirming this harness can reproduce the bug too", lengths)
	}
}

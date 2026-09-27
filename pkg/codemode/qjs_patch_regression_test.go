package codemode

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestRunCodeMode_LiteralStringLengths_ByteExact is a regression test for a
// use-after-free bug in the vendored QuickJS engine
// (pkg/internal/third_party/qjs), found while proving the ReadString
// NUL-scanning fix (see run_code_mode_test.go's
// TestRunCodeMode_ManyInvocationsRemainCorrect and the codemode_stress-gated
// stress test) with a broader length sweep than those two exercise.
//
// The QJS_JSONStringify WASM helper (qjs.wasm's helpers.c) frees the string
// buffer whose address and length it packs and returns, before ever
// returning it -- see qjs's Value.JSONStringify doc comment and
// README.vendor.md for the full explanation and fix (bypassing that helper
// via JS-level JSON.stringify + the safe QJS_ToCString-based String()
// read). Depending on how the WASM libc allocator's free-list/boundary-tag
// bookkeeping lands in the freed chunk, this could silently corrupt or
// truncate an otherwise-correct result. Empirically, before the fix, this
// reproduced for literal string lengths 3-5, 67-69, and, deterministically,
// every length from 214 up through at least 400 -- so this sweep spans
// those exact ranges plus neighboring "should be fine either way" lengths,
// rather than only a couple of spot checks.
func TestRunCodeMode_LiteralStringLengths_ByteExact(t *testing.T) {
	lengths := []int{
		0, 1, 2, 3, 4, 5, 6, 7, 10, 50,
		66, 67, 68, 69, 70,
		99, 100, 101, 127, 128, 129, 150, 200,
		213, 214, 215, 216, 255, 256, 257, 300, 350, 400,
	}

	for _, n := range lengths {
		s := strings.Repeat("a", n)
		js := fmt.Sprintf("return %q;", s)
		got, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: ToolSet{}})
		if err != nil {
			t.Fatalf("len %d: unexpected error: %v", n, err)
		}
		gotStr, ok := got.(string)
		if !ok || gotStr != s {
			t.Fatalf("len %d: got %#v, want a %d-byte string of 'a'", n, got, n)
		}
	}
}

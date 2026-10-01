package qjs

import (
	"bytes"
	"context"
	"testing"
)

// TestReadString_TrustsReportedSizeNotNULScan is a regression test for the
// vendoring patch documented in ReadString's doc comment and
// README.vendor.md.
//
// Upstream v0.0.6 located a returned string's end by scanning the read
// window for a NUL byte (bytes.IndexByte) instead of trusting the exact
// byte length QuickJS reported for the string (the size half of a packed
// pointer, from UnpackPtr). Any stray zero byte within that reported range
// -- an embedded NUL in the string itself, or, as observed in production
// with esbuild interleaved in the same process, a stale zero byte left in
// reused WASM linear memory -- silently truncated the result.
//
// This writes a buffer whose reported length spans an embedded NUL byte and
// asserts the patched ReadString and StringFromPackedPtr return the full
// content unmodified. It then re-derives what the *unpatched* upstream
// algorithm would have returned for the exact same buffer (a local copy of
// its NUL-scan logic, not a hand-picked expectation) and asserts that
// result really is truncated -- so this test would fail against the
// unpatched logic and only passes with the patch.
func TestReadString_TrustsReportedSizeNotNULScan(t *testing.T) {
	rt, err := New(Option{Context: context.Background()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeRuntimeForTest(rt)

	mem := rt.Mem()

	// The exact bug report is "16384 -> 1638" (last byte dropped because a
	// stray NUL landed where the last digit should be); this reproduction
	// goes further per the task's guidance and plants a real NUL well
	// *inside* the reported size range, with plenty of non-NUL content
	// after it, so any truncation is unmistakable rather than off-by-one.
	content := []byte("16384-more-data-after-an-embedded\x00-nul-byte-right-here-16384")
	reportedLen := uint32(len(content))

	addr := uint32(rt.Malloc(uint64(reportedLen)))
	defer rt.FreeHandle(uint64(addr))

	if werr := mem.Write(addr, content); werr != nil {
		t.Fatalf("Write: %v", werr)
	}

	// --- Patched behavior: ReadString trusts reportedLen exactly. ---
	got, rerr := mem.ReadString(addr, reportedLen)
	if rerr != nil {
		t.Fatalf("ReadString: %v", rerr)
	}
	if got != string(content) {
		t.Fatalf("ReadString: got %q (len %d), want %q (len %d)", got, len(got), string(content), len(content))
	}

	// --- Patched behavior via the real production path used by every
	// Value.String()/JSONStringify() call: StringFromPackedPtr. ---
	packedAddr := uint32(rt.Malloc(PackedPtrSize))
	defer rt.FreeHandle(uint64(packedAddr))
	packed := uint64(addr)<<32 | uint64(reportedLen)
	if werr := mem.WriteUint64(packedAddr, packed); werr != nil {
		t.Fatalf("WriteUint64: %v", werr)
	}
	got2 := mem.StringFromPackedPtr(uint64(packedAddr))
	if got2 != string(content) {
		t.Fatalf("StringFromPackedPtr: got %q (len %d), want %q (len %d)", got2, len(got2), string(content), len(content))
	}

	// --- Contrast: what the UNPATCHED (upstream v0.0.6) ReadString did for
	// this exact buffer. This is upstream's algorithm verbatim: read up to
	// (roughly) reportedLen+1 bytes -- see calculateSafeReadLength, removed
	// by the patch -- and stop at the first NUL, discarding the reported
	// length as anything but an upper bound.
	unpatchedWindow := content // upstream read min(available, reportedLen+1) bytes; content already covers reportedLen and contains no trailing bytes beyond it, so this is that window.
	nullIndex := bytes.IndexByte(unpatchedWindow, StringTerminator)
	if nullIndex < 0 {
		t.Fatalf("test buffer must contain an embedded NUL for this reproduction to be meaningful")
	}
	unpatchedResult := string(unpatchedWindow[:nullIndex])

	if unpatchedResult == string(content) {
		t.Fatalf("expected the unpatched NUL-scan to truncate the string, but it reproduced the full content -- this test no longer reproduces the original bug")
	}
	if len(unpatchedResult) >= len(content) {
		t.Fatalf("expected the unpatched result to be shorter than the full content (truncated at the embedded NUL): got len %d, full len %d", len(unpatchedResult), len(content))
	}

	// And the patched result must differ from -- specifically, be longer
	// than -- what the unpatched algorithm would have produced.
	if got == unpatchedResult {
		t.Fatalf("patched ReadString reproduced the unpatched (truncated) result %q; the fix did not take effect", unpatchedResult)
	}
}

// TestReadString_EmptyLength mirrors upstream's "zero_maxlen_handling" case:
// a zero reported length is not an error, it is an empty string, with no
// memory access attempted.
func TestReadString_EmptyLength(t *testing.T) {
	rt, err := New(Option{Context: context.Background()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeRuntimeForTest(rt)

	addr := uint32(rt.Malloc(8))
	defer rt.FreeHandle(uint64(addr))

	got, rerr := rt.Mem().ReadString(addr, 0)
	if rerr != nil {
		t.Fatalf("ReadString(0): unexpected error: %v", rerr)
	}
	if got != "" {
		t.Fatalf("ReadString(0): got %q, want empty string", got)
	}
}

// closeRuntimeForTest closes a Runtime created with a background context
// (so CloseOnContextDone is inert here; see engine.go's doc comment on the
// process-global compiled-module cache for why that flag only matters when
// set on the very first qjs.New call in the process). Runtime.Close can
// still legitimately be noisy in some failure paths upstream, so this stays
// defensive like the codemode engine's own cleanup.
func closeRuntimeForTest(rt *Runtime) {
	defer func() { _ = recover() }()
	rt.Close()
}

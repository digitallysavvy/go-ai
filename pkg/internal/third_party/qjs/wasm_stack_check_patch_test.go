package qjs

import (
	"context"
	"strings"
	"testing"
)

// Regression test for build/wasm-stack-check.patch (README.vendor.md,
// "Catchable stack overflow"): upstream disables QuickJS's C stack check on
// wasi, so runaway JS recursion used to walk off the wasm shadow stack and
// trap the whole instance ("out of bounds memory access"). With the patch,
// recursion throws a catchable `InternalError: stack overflow` in-script.
func TestWasmStackCheck_RecursionThrowsCatchableStackOverflow(t *testing.T) {
	for _, tc := range []struct{ name, js string }{
		{"deep", `function f(n){ return f(n+1)+1 }
			(() => { try { f(0); return 'none'; } catch (e) { return e.name + ': ' + e.message; } })()`},
		{"mutual", `function a(n){ return b(n)+1 } function b(n){ return a(n)+1 }
			(() => { try { a(0); return 'none'; } catch (e) { return e.name + ': ' + e.message; } })()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := New(Option{Context: context.Background(), NoFSMount: true, MaxStackSize: 2 * 1024 * 1024})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer rt.Close()

			v, err := rt.Context().Eval("probe.js", Code(tc.js))
			if err != nil {
				t.Fatalf("recursion must be catchable in-script, got host error: %.300s", err)
			}
			defer v.Free()
			if got := v.String(); got != "InternalError: stack overflow" {
				t.Fatalf("got %q, want %q", got, "InternalError: stack overflow")
			}

			// The same runtime stays usable afterwards.
			v2, err := rt.Context().Eval("after.js", Code(`1+1`))
			if err != nil {
				t.Fatalf("eval after overflow: %v", err)
			}
			defer v2.Free()
			if v2.String() != "2" {
				t.Fatalf("after overflow got %q", v2.String())
			}
		})
	}
}

// Without a configured limit (MaxStackSize 0) the patch must still bound
// recursion rather than leave the check disabled.
func TestWasmStackCheck_DefaultLimitStillApplies(t *testing.T) {
	rt, err := New(Option{Context: context.Background(), NoFSMount: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rt.Close()
	_, err = rt.Context().Eval("p.js", Code(`function f(){ return f()+1 } f()`))
	if err == nil || !strings.Contains(err.Error(), "stack overflow") || strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("want stack overflow script error, got %v", err)
	}
}

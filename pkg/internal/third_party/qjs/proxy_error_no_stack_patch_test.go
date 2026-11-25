package qjs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestProxyPanicErrorCarriesNoGoStack is the regression test for the
// "No Go stack in script-visible errors" patch (see README.vendor.md):
// upstream appended debug.Stack() to the error built when a Go function
// called from JS panics (newProxyErr), and to the error raised when a WASM
// call fails (Runtime.call). That text reaches sandboxed scripts and, through
// pkg/codemode, a model, so it must not carry goroutine dumps or host source
// paths. A re-sync from upstream that brings debug.Stack() back fails here.
func TestProxyPanicErrorCarriesNoGoStack(t *testing.T) {
	for name, panicValue := range map[string]any{
		"error":  errors.New("boom"),
		"string": "boom",
		"other":  42,
	} {
		t.Run(name, func(t *testing.T) {
			rt, err := New(Option{Context: context.Background(), NoFSMount: true, DisableModules: true})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer rt.Close()
			jsCtx := rt.Context()
			jsCtx.SetFunc("boom", func(*This) (*Value, error) { panic(panicValue) })

			v, err := jsCtx.Eval("probe.js", Code(`
				let msg = 'no error';
				try { boom(); } catch (e) { msg = String(e && e.message) + '\n' + String(e && e.stack); }
				msg;
			`))
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			msg := v.String()
			if !strings.Contains(msg, "functionProxy") {
				t.Fatalf("expected the proxy panic error to reach the script, got %q", msg)
			}
			for _, leak := range []string{"goroutine ", ".go:", "runtime/debug"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("script-visible error leaks %q:\n%.500s", leak, msg)
				}
			}
		})
	}
}

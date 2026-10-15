package codemode

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Runaway recursion must surface inside the script as a catchable
// stack-overflow error (parity with TS code-mode), not as an engine trap.
// Guards the wasm-stack-check.patch in pkg/internal/third_party/qjs/build.
func TestStackOverflowIsCatchableInScript(t *testing.T) {
	cases := map[string]string{
		"deep": `function f(n){ return f(n+1)+1 }
			try { f(0); return 'no-error'; } catch (e) { return 'caught:' + String(e && e.message) + '|' + (e instanceof Error) + '|' + (e && e.name); }`,
		"mutual": `function a(n){ return b(n)+1 } function b(n){ return a(n)+1 }
			try { a(0); return 'no-error'; } catch (e) { return 'caught:' + String(e && e.message) + '|' + (e instanceof Error) + '|' + (e && e.name); }`,
		"arrow-method": `const o = { m(){ return [1].map(() => this.m())[0] } };
			try { o.m(); return 'no-error'; } catch (e) { return 'caught:' + String(e && e.message) + '|' + (e instanceof Error) + '|' + (e && e.name); }`,
	}
	for name, js := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := RunCodeMode(context.Background(), RunInput{JS: js})
			if err != nil {
				t.Fatalf("recursion must be catchable in-script, got host error: %.300s", err)
			}
			s, _ := got.(string)
			if !strings.HasPrefix(s, "caught:") || !strings.Contains(s, "stack overflow") || !strings.HasSuffix(s, "|true|InternalError") {
				t.Fatalf("result %q, want a caught 'stack overflow' error", s)
			}
			for _, leak := range []string{"goroutine", ".go:", "wasm stack trace", "runtime/debug", "out of bounds"} {
				if strings.Contains(s, leak) {
					t.Fatalf("error leaks %q: %.300s", leak, s)
				}
			}
		})
	}
}

// After a caught overflow the script (and its host-function bridge) must
// keep working, and repeated overflows must not poison later runs.
func TestScriptContinuesAfterCaughtStackOverflow(t *testing.T) {
	js := `function f(){ return f()+1 }
		let n = 0;
		for (let i = 0; i < 3; i++) { try { f(); } catch (e) { n++; } }
		function sum(k){ return k === 0 ? 0 : k + sum(k-1) }
		return { overflows: n, sum: sum(500) };`
	got, err := RunCodeMode(context.Background(), RunInput{JS: js})
	if err != nil {
		t.Fatalf("unexpected error: %.300s", err)
	}
	m, ok := got.(map[string]any)
	if !ok || fmt.Sprint(m["overflows"]) != "3" || fmt.Sprint(m["sum"]) != "125250" {
		t.Fatalf("got %#v", got)
	}
	// A fresh run right after must be healthy too.
	if got, err := RunCodeMode(context.Background(), RunInput{JS: "return 1+1"}); err != nil || fmt.Sprint(got) != "2" {
		t.Fatalf("follow-up run: %v %v", got, err)
	}
}

// Uncaught overflow is a plain script error: no engine trap, no host trace.
func TestUncaughtStackOverflowIsScriptErrorWithoutHostTrace(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{JS: "function f(){ return f()+1 } return f();"})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "stack overflow") || strings.Contains(msg, "trapped") {
		t.Fatalf("want stack overflow script error, got %.300s", msg)
	}
	for _, leak := range []string{"goroutine", ".go:", "wasm stack trace", "runtime/debug"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaks %q: %.300s", leak, msg)
		}
	}
}

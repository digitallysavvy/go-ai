package codemode

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A panicking host tool runs on its own goroutine; it must surface as a tool
// failure inside the script instead of crashing the process.
func TestHostToolPanicBecomesToolError(t *testing.T) {
	tools := ToolSet{"boom": {
		Name:       "boom",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			var m map[string]int
			m["x"] = 1
			return nil, nil
		},
	}}
	got, err := RunCodeMode(context.Background(), RunInput{
		JS:    "try { await tools.boom({}); } catch (e) { return String(e.message); } return 'no error';",
		Tools: tools,
	})
	if err != nil || got != "Host tool failed." {
		t.Fatalf("got %#v, err %v", got, err)
	}
}

// Runaway recursion traps the engine; the error must not carry the host's Go
// stack (source paths) or the engine's frame dump.
func TestRunawayRecursionErrorDoesNotLeakHostTrace(t *testing.T) {
	_, err := RunCodeMode(context.Background(), RunInput{JS: "function f(){ return f()+1 } return f();"})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, leak := range []string{"goroutine", ".go:", "wasm stack trace", "runtime/debug"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaks %q: %.300s", leak, msg)
		}
	}
	if !strings.HasPrefix(msg, "InternalError:") && !strings.Contains(msg, "stack overflow") {
		t.Fatalf("unexpected error: %.300s", msg)
	}
}

// The static import() scan tokenizes loosely, and a construct the scanner
// reads as a regular expression can hide an import call from it. The engine's
// own module lockdown must still refuse the load, whichever spelling reaches it.
func TestHiddenDynamicImportIsStillRefusedByTheEngine(t *testing.T) {
	for _, spec := range []string{"qjs:std", "qjs:os", "qjs:bjson"} {
		js := "let v; const x = {} / (v = import /*c*/ ('" + spec + "')) / 1;\n" +
			"try { await v; return 'LOADED'; } catch (e) { return String(e.message); }"
		got, err := RunCodeMode(context.Background(), RunInput{JS: js})
		if err != nil {
			continue // rejected up front by the static scan: also fine
		}
		if s, _ := got.(string); !strings.Contains(s, "module imports are disabled") {
			t.Fatalf("%s: result %#v, want the module lockdown error", spec, got)
		}
	}
}

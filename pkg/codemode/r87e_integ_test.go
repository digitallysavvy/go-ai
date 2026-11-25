package codemode

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS run-code-mode.ts: `tools` reads __codeModeBindings, an object literal,
// so an Object.prototype function that is not a tool is called with this
// undefined (no tool call), while a registered tool of the same name is
// still the tool (cr-r87e-tools-to-integrator 2).
func TestR87eintegToolsObjectPrototypeNames(t *testing.T) {
	var calls atomic.Int32
	echo := types.Tool{Name: "toString", Parameters: r87ccarryObject,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			calls.Add(1)
			return "tool", nil
		}}
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return await tools.toString();", Tools: ToolSet{}})
	if err != nil || got != "[object Undefined]" {
		t.Fatalf("toString = %#v, %v", got, err)
	}
	_, err = RunCodeMode(context.Background(), RunInput{JS: `return await tools.hasOwnProperty("a");`, Tools: ToolSet{}})
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("hasOwnProperty err = %v, want a TypeError", err)
	}
	got, err = RunCodeMode(context.Background(), RunInput{JS: "return await tools.toString({});", Tools: ToolSet{"toString": echo}})
	if err != nil || got != "tool" || calls.Load() != 1 {
		t.Fatalf("registered toString = %#v, %v (calls %d)", got, err, calls.Load())
	}
}

// TS run serializes host-call arguments with devalue in the proxy's apply
// trap and throws `Host function arguments are not serializable: <devalue
// message>` synchronously, before any request; values devalue accepts reach
// the tool (cr-r87e-tools-to-integrator 4).
func TestR87eintegHostArgumentsDevalueErrors(t *testing.T) {
	var calls atomic.Int32
	var lastInput map[string]interface{}
	tools := ToolSet{"a": {Name: "a", Parameters: r87ccarryObject,
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			calls.Add(1)
			lastInput = in
			return "ok", nil
		}}}
	for js, want := range map[string]string{
		"return await tools.a(() => 1);":                   "Cannot stringify a function",
		"return await tools.a({ s: Symbol('x') });":        "Cannot stringify a Symbol primitive",
		"return await tools.a(new (class X {})());":        "Cannot stringify arbitrary non-POJOs",
		"return await tools.a({ [Symbol('k')]: 1 });":      "Cannot stringify POJOs with symbolic keys",
		"return await tools.a({ p: Promise.resolve(1) });": "Cannot stringify a Promise or thenable — use stringifyAsync instead",
	} {
		_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
		if err == nil || !strings.Contains(err.Error(), "Host function arguments are not serializable: "+want) {
			t.Errorf("%s: err = %v, want %q", js, err, want)
		}
	}
	got, err := RunCodeMode(context.Background(), RunInput{JS: `try { tools.a(() => 1); } catch (e) { return e instanceof TypeError && e.code; }`, Tools: tools})
	if err != nil || got != "RUN_SERIALIZATION_ERROR" {
		t.Errorf("synchronous throw = %#v, %v", got, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("tool ran %d times for unserializable input", calls.Load())
	}
	got, err = RunCodeMode(context.Background(), RunInput{JS: `return await tools.a({ e: new Error("x"), m: new Map([[1, 2]]), d: 1 });`, Tools: tools})
	if err != nil || got != "ok" || calls.Load() != 1 || lastInput["d"] != float64(1) {
		t.Fatalf("accepted input = %#v, %v (calls %d, input %v)", got, err, calls.Load(), lastInput)
	}
}

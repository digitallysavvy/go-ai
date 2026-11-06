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

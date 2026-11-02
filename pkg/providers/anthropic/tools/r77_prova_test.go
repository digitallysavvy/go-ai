package tools

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r77provaLegacyRuntime struct{ called bool }

func (r *r77provaLegacyRuntime) ExecuteCommand(context.Context, string, bool) (interface{}, error) {
	r.called = true
	return "ran", nil
}

// TS bash_20250124.ts / bash_20241022.ts default execute: `if (!sandbox)
// throw new Error('Sandbox session is not available')`. A nil *T that
// implements SandboxSession (or the v0.5 RuntimeContext ExecuteCommand
// sandbox) is a missing sandbox, not a receiver to call Run on.
func TestR77prova_BashTypedNilSandboxIsMissing(t *testing.T) {
	for name, tool := range map[string]types.Tool{"20250124": Bash20250124(), "20241022": Bash20241022()} {
		t.Run(name, func(t *testing.T) {
			var nilSession *r76provaSession
			var nilRuntime *r77provaLegacyRuntime
			for label, opts := range map[string]types.ToolExecutionOptions{
				"session":        {ExperimentalSandbox: nilSession},
				"runtimeContext": {RuntimeContext: nilRuntime},
				"both":           {ExperimentalSandbox: nilSession, RuntimeContext: nilRuntime},
			} {
				_, err := tool.Execute(t.Context(), map[string]interface{}{"command": "ls"}, opts)
				if err == nil || err.Error() != "Sandbox session is not available" {
					t.Fatalf("%s: err = %v", label, err)
				}
			}

			// A nil session still falls through to a real legacy runtime.
			runtime := &r77provaLegacyRuntime{}
			out, err := tool.Execute(t.Context(), map[string]interface{}{"command": "ls"}, types.ToolExecutionOptions{ExperimentalSandbox: nilSession, RuntimeContext: runtime})
			if err != nil || out != "ran" || !runtime.called {
				t.Fatalf("out = %v, err = %v, called = %v", out, err, runtime.called)
			}
		})
	}
}

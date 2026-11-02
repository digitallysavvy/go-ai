package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r77carryToolModel asks for tool "a" on the first call and answers "done"
// afterwards, in both modes, recording each call's options.
func r77carryToolModel() (*testutil.MockLanguageModel, *[]*provider.GenerateOptions) {
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	record := func(o *provider.GenerateOptions) int {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, o)
		return len(calls)
	}
	call := types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}
	m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true}
	m.DoGenerateFunc = func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		if record(o) == 1 {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}
	m.DoStreamFunc = func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		if record(o) == 1 {
			c := call
			return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &c}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
	}
	return m, &calls
}

// TS model-call-iterator.ts: prepareStep has no `tools` key and applies
// `experimental_sandbox ?? sandbox`; prepareCall has neither key. A hook that
// returns a fresh LanguageModelCallOptions (every field nil) keeps the tools
// and the call's sandbox, in both modes and for both hooks.
func TestR77Carry_HookNilToolsAndSandboxKeepCurrent(t *testing.T) {
	fresh := func(context.Context, LanguageModelCallOptions) (LanguageModelCallOptions, error) {
		return LanguageModelCallOptions{}, nil
	}
	sandbox := &struct{ name string }{"sb"}
	for _, hookKind := range []string{"prepareStep", "prepareCall"} {
		for _, mode := range []string{"stream", "generate"} {
			t.Run(hookKind+"/"+mode, func(t *testing.T) {
				model, calls := r77carryToolModel()
				var mu sync.Mutex
				var gotSandbox interface{}
				executed := false
				cfg := WorkflowAgent{Model: model, Tools: []types.Tool{{
					Name: "a", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
					Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
						mu.Lock()
						defer mu.Unlock()
						executed, gotSandbox = true, o.ExperimentalSandbox
						return "ok", nil
					},
				}}}
				if hookKind == "prepareStep" {
					cfg.PrepareStep = fresh
				} else {
					cfg.PrepareCall = fresh
				}
				a, err := NewWorkflowAgent(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "generate" {
					if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "x", ExperimentalSandbox: sandbox}); err != nil {
						t.Fatal(err)
					}
				} else {
					st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "x", ExperimentalSandbox: sandbox})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := st.ReadAll(); err != nil {
						t.Fatal(err)
					}
				}
				if len(*calls) == 0 || len((*calls)[0].Tools) != 1 {
					t.Fatalf("first model call tools = %+v, want [a]", (*calls)[0].Tools)
				}
				mu.Lock()
				defer mu.Unlock()
				if !executed {
					t.Fatal("tool a was not executed")
				}
				if gotSandbox != sandbox {
					t.Errorf("tool sandbox = %v, want the call's sandbox", gotSandbox)
				}
			})
		}
	}
}

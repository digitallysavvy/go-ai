package workflow

import (
	"context"
	"reflect"
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

// TS resolveSerializableTools: a `type: 'provider'` def gets jsonSchema()
// without a validator even when it has no id (Go rebuilds it as a function
// tool, but its input still passes unvalidated). A plain function def with
// the same schema still fails.
func TestR77Carry_ProviderDefWithoutIDIsNotValidated(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"x": map[string]interface{}{"type": "string"}}, "required": []interface{}{"x"}}
	tools := ResolveSerializableTools(map[string]SerializableToolDef{
		"p": {Type: "provider", InputSchema: schema},
		"f": {InputSchema: schema},
	})
	byName := map[string]types.Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	in := map[string]interface{}{}
	if got, err := byName["p"].ValidateInput(in); err != nil || got == nil {
		t.Errorf("provider def without id: ValidateInput = %v, %v; want the input unchanged", got, err)
	}
	if _, err := byName["f"].ValidateInput(in); err == nil || err.Error() != `data is missing required property "x"` {
		t.Errorf("function def: err = %v, want the Ajv missing-property error", err)
	}
}

// TS Ajv validates JSON values. A Go caller's tool input (for example one
// revalidated from an approval message it built) can hold typed slices and
// maps; the resolved tool's ValidateInput accepts them as the arrays and
// objects they encode to, and returns the input unchanged.
func TestR77Carry_ValidateInputAcceptsGoTypedJSONValues(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"n":    map[string]interface{}{"type": "integer"},
		"o":    map[string]interface{}{"type": "object", "required": []interface{}{"a"}},
	}}
	tools := ResolveSerializableTools(map[string]SerializableToolDef{"f": {InputSchema: schema}})
	in := map[string]interface{}{"tags": []string{"a"}, "n": 3, "o": map[string]string{"a": "b"}}
	got, err := tools[0].ValidateInput(in)
	if err != nil {
		t.Fatalf("ValidateInput(Go-typed input) = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("ValidateInput returned %#v, want the input unchanged", got)
	}
	if _, err := tools[0].ValidateInput(map[string]interface{}{"tags": []int{1}}); err == nil || err.Error() != "data/tags/0 must be string" {
		t.Errorf("ValidateInput([]int tags) err = %v, want data/tags/0 must be string", err)
	}
}

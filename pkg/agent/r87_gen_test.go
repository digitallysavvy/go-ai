package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR87genAgentStepRequestURLOrigin: GenerateAgent's loop step request carries
// the sanitized origin of the provider-reported URL, as TS generateText's
// step request does (934100cad4).
func TestR87genAgentStepRequestURLOrigin(t *testing.T) {
	model := &functionalAgentLanguageModel{
		modelID: "m",
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				RequestURL:   "https://user:password@model.example.test:8443/private?key=secret#fragment",
			}, nil
		},
	}
	result, err := NewToolLoopAgent(AgentConfig{Model: model}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) == 0 {
		t.Fatal("no steps")
	}
	for i, s := range result.Steps {
		if s.Request.URL != "https://model.example.test:8443" {
			t.Errorf("step %d request URL = %q", i, s.Request.URL)
		}
	}
}

// TestR87genAgentToModelOutputErrorRunsRemainingCalls: as GenerateText
// (cr-r87-stream-to-gen 3), the agent loop with ToolCallConcurrency below
// the call count runs every call even when the first ToModelOutput fails.
func TestR87genAgentToModelOutputErrorRunsRemainingCalls(t *testing.T) {
	one := 1
	var mu sync.Mutex
	var executed []string
	execute := func(name string) types.ToolExecutor {
		return func(ctx context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			executed = append(executed, name)
			mu.Unlock()
			return "ok", nil
		}
	}
	convErr := errors.New("toModelOutput failed")
	model := &functionalAgentLanguageModel{
		modelID: "m",
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{
					{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}},
					{ID: "c2", ToolName: "u", Arguments: map[string]interface{}{}},
				},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{
			{Name: "t", Execute: execute("t"), ToModelOutput: func(ctx context.Context, _ types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				return nil, convErr
			}},
			{Name: "u", Execute: execute("u")},
		},
		ToolCallConcurrency: &one,
	})
	_, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if !errors.Is(err, convErr) {
		t.Fatalf("err = %v, want the conversion error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(executed, []string{"t", "u"}) {
		t.Errorf("executed %v, want [t u]", executed)
	}
}

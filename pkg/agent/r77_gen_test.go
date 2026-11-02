package agent

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS WorkflowAgent (generate) replays the tool input callbacks with
// stepInputMessages, the step's conversation prompt (system message first):
// with toolMessagesAsConversationPrompt the legacy loop's OnInputStart and
// OnInputAvailable get the model prompt, like the tool start event;
// without it, the step messages.
func TestR77GenAgentInputCallbacksGetConversationPrompt(t *testing.T) {
	var startGot, availableGot []types.Message
	tool := types.Tool{Name: "search", Parameters: map[string]interface{}{"type": "object"},
		OnInputStart: func(_ context.Context, o types.OnInputStartOptions) error {
			startGot = o.Messages
			return nil
		},
		OnInputAvailable: func(_ context.Context, o types.OnInputAvailableOptions) error {
			availableGot = o.Messages
			return nil
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}
	call := types.ToolCall{ID: "search-1", ToolName: "search", Arguments: map[string]interface{}{}}
	roles := func(ms []types.Message) []types.MessageRole {
		var out []types.MessageRole
		for _, m := range ms {
			out = append(out, m.Role)
		}
		return out
	}
	for _, on := range []bool{false, true} {
		startGot, availableGot = nil, nil
		a := NewToolLoopAgent(AgentConfig{Model: r52ToolCallModel(call), Tools: []types.Tool{tool}, System: "sys"})
		user := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}}
		if _, err := a.executeWithMessages(context.Background(), user, agentCallbacks{toolMessagesAsConversationPrompt: on}); err != nil {
			t.Fatal(err)
		}
		want := []types.MessageRole{types.RoleUser}
		if on {
			want = []types.MessageRole{types.RoleSystem, types.RoleUser}
		}
		if !reflect.DeepEqual(roles(startGot), want) || !reflect.DeepEqual(roles(availableGot), want) {
			t.Fatalf("on=%v: start %v available %v, want %v", on, roles(startGot), roles(availableGot), want)
		}
	}
}

// TS generateText resolves a pending deferred provider call on any
// tool-result part of the model response, an isError one included
// (pendingDeferredToolCalls reads the raw response content). The legacy loop
// read the step content, where an isError result is already a tool-error
// part, so a deferred call answered with an error kept the run going.
func TestR77GenAgentDeferredProviderErrorResultResolves(t *testing.T) {
	isErr := true
	var calls atomic.Int32
	model := &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls.Add(1)
			call := types.ToolCall{ID: "srv-1", ToolName: "code_execution", Arguments: map[string]interface{}{}, ProviderExecuted: true, ProviderExecutedSet: true}
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{call},
				Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "srv-1", ToolName: "code_execution", Arguments: map[string]interface{}{}, ProviderExecuted: true},
					types.ToolResultContent{ToolCallID: "srv-1", ToolName: "code_execution", Result: map[string]interface{}{"error": "boom"}, IsError: &isErr, ProviderExecuted: true},
				},
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}
	tool := types.Tool{Name: "code_execution", Type: types.ToolTypeProviderDefined, ProviderExecuted: true, SupportsDeferredResults: true}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}})
	r, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 || len(r.Steps) != 1 {
		t.Fatalf("model calls = %d, steps = %d, want 1 (the deferred call was answered)", n, len(r.Steps))
	}
}

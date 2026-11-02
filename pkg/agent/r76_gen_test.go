package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// R76 item 2 (carry CR cr-r76-carry-to-gen.md): the legacy loop's executed
// client tool results drop the call's provider metadata when
// withoutCallMetadata is set (WorkflowAgent.Generate; TS WorkflowAgent
// executeTool), and copy it otherwise (TS core).
func TestR76GenAgentClientToolResultsWithoutCallMetadata(t *testing.T) {
	md := map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig"}}
	tool := types.Tool{Name: "ok", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}
	a := NewToolLoopAgent(AgentConfig{Model: r52ToolCallModel(), Tools: []types.Tool{tool}})
	call := types.ToolCall{ID: "c1", ToolName: "ok", Arguments: map[string]interface{}{}, ProviderMetadata: md}
	for _, without := range []bool{false, true} {
		results, err := a.executeTools(context.Background(), []types.ToolCall{call}, []types.Tool{tool}, nil, 0, nil, nil, nil, nil, nil, agentCallbacks{withoutCallMetadata: without}, types.Usage{}, nil)
		if err != nil || len(results) != 1 {
			t.Fatalf("results %v err %v", results, err)
		}
		got := results[0].ProviderMetadata
		if without && got != nil {
			t.Fatalf("without: ProviderMetadata = %v, want nil", got)
		}
		if !without && !reflect.DeepEqual(got, md) {
			t.Fatalf("default: ProviderMetadata = %v, want the call's", got)
		}
	}
}

// Owner ruling 2026-11-01 (decision 1, option c): the legacy loop validates a
// tool with ValidateInput through it (ai.ParseToolCall), with the TS
// TypeValidationError wording in the tool error.
func TestR76GenAgentToolValidateInput(t *testing.T) {
	executed := 0
	tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"},
		ValidateInput: func(input interface{}) (interface{}, error) {
			return nil, errors.New("data must have required property 'value'")
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed++
			return "ok", nil
		}}
	a := NewToolLoopAgent(AgentConfig{Model: r52ToolCallModel(types.ToolCall{ID: "c1", ToolName: "tool1", RawArguments: `{"a":1}`}), Tools: []types.Tool{tool}})
	r, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if executed != 0 {
		t.Fatal("invalid call executed")
	}
	want := "AI_InvalidToolInputError: Invalid input for tool tool1: AI_TypeValidationError: Type validation failed: Value: {\"a\":1}.\nError message: Error: data must have required property 'value'"
	for _, p := range r.Steps[0].Content {
		if te, ok := p.(types.ToolErrorContent); ok {
			if te.ErrorText != want {
				t.Fatalf("ErrorText = %q\nwant %q", te.ErrorText, want)
			}
			return
		}
	}
	t.Fatalf("no tool error in %#v", r.Steps[0].Content)
}

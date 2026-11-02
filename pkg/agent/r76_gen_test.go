package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
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

// Carry/stream CR item C (legacy loop, WorkflowAgent.Generate): with
// toolMessagesAsConversationPrompt, Execute and the approval function get the
// step's model prompt (system first) plus the step's assistant message, and
// the tool start event the prompt alone (TS executeToolWithCallbacks
// iterMessages / getToolCallbackMessages); without it, the step messages.
func TestR76GenAgentToolMessagesAsConversationPrompt(t *testing.T) {
	var execGot, approvalGot, startGot []types.Message
	tool := types.Tool{Name: "search", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			execGot = o.Messages
			return "ok", nil
		}}
	call := types.ToolCall{ID: "search-1", ToolName: "search", Arguments: map[string]interface{}{}}
	for _, on := range []bool{false, true} {
		a := NewToolLoopAgent(AgentConfig{Model: r52ToolCallModel(call), Tools: []types.Tool{tool}, System: "sys",
			ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
				approvalGot = o.Messages
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
			})})
		user := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}}
		_, err := a.executeWithMessages(context.Background(), user, agentCallbacks{
			toolMessagesAsConversationPrompt: on,
			onToolCallStart:                  func(_ context.Context, e ai.OnToolCallStartEvent) { startGot = e.Messages },
		})
		if err != nil {
			t.Fatal(err)
		}
		if !on {
			if len(execGot) != 1 || len(approvalGot) != 1 || len(startGot) != 1 || execGot[0].Role != types.RoleUser {
				t.Fatalf("off: exec %d approval %d start %d", len(execGot), len(approvalGot), len(startGot))
			}
			continue
		}
		roles := func(ms []types.Message) []types.MessageRole {
			var out []types.MessageRole
			for _, m := range ms {
				out = append(out, m.Role)
			}
			return out
		}
		want := []types.MessageRole{types.RoleSystem, types.RoleUser, types.RoleAssistant}
		if !reflect.DeepEqual(roles(execGot), want) || !reflect.DeepEqual(roles(approvalGot), want) {
			t.Fatalf("on: exec %v approval %v, want %v", roles(execGot), roles(approvalGot), want)
		}
		if !reflect.DeepEqual(roles(startGot), want[:2]) {
			t.Fatalf("on: start %v, want %v", roles(startGot), want[:2])
		}
		if len(execGot[2].ToolCalls) != 1 || execGot[2].ToolCalls[0].ID != "search-1" {
			t.Fatalf("assistant message = %#v", execGot[2])
		}
	}
}

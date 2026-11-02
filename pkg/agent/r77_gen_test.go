package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

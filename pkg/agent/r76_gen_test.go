package agent

import (
	"context"
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

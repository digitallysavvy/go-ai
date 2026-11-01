package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The resume start-gate chain is transitive (r74 ruling, as pkg/ai and the
// legacy loop): call-2 returns early (input revalidation fails), so it must
// wait for call-1's start before releasing call-3. Otherwise call-3 fires
// its start while call-1 is still revalidating, and TS mapWithConcurrency
// order (call-1, call-3) is lost.
func TestR75Carry_ApprovedToolStartGateTransitiveOverEarlyReturn(t *testing.T) {
	call3Started := make(chan struct{})
	var call3Once sync.Once
	var mu sync.Mutex
	var starts []string
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "tool1",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"n": map[string]interface{}{"type": "number"}},
				"required":   []interface{}{"n"},
			},
			// call-1's approval policy is slow: it waits until call-3 has
			// fired its start (the bug) or a short timeout (the fix: call-3
			// cannot start before call-1).
			ToolApproval: types.ToolNeedsApprovalFunc(func(_ context.Context, _ map[string]interface{}, o types.ToolNeedsApprovalOptions) bool {
				if o.ToolCallID == "call-1" {
					select {
					case <-call3Started:
					case <-time.After(300 * time.Millisecond):
					}
				}
				return true
			}),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			mu.Lock()
			starts = append(starts, e.ToolCall.ID)
			mu.Unlock()
			if e.ToolCall.ID == "call-3" {
				call3Once.Do(func() { close(call3Started) })
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := r74carryApprovedMessages("call-1", "call-2", "call-3")
	msgs[1].Content[0] = types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"n": 1}}
	// call-2 has invalid input: revalidation fails before any start callback.
	msgs[1].Content[4] = types.ToolCallContent{ToolCallID: "call-3", ToolName: "tool1", Arguments: map[string]interface{}{"n": 3}}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Steps()
	mu.Lock()
	got := strings.Join(starts, ",")
	mu.Unlock()
	if got != "call-1,call-3" {
		t.Errorf("start order = %s, want call-1,call-3", got)
	}
	if tr := lastToolResultInPrompt(t, model, "call-2"); tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText {
		t.Errorf("call-2 result = %+v, want error-text", tr.Output)
	}
}

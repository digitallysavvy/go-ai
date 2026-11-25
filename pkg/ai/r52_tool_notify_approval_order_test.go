package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS generateText notifies each valid call's tool (onInputStart, then
// onInputAvailable) and resolves that call's approval before moving to the
// next call, so approval functions and input callbacks interleave per call.
// (TS streamText runs onInputAvailable in an upstream transform after
// enqueueing the chunk, so its order against the downstream approval is a
// matter of microtask scheduling; only generateText's order is fixed.)
func TestR52ToolInputCallbacksAndApprovalsInterleavePerCall(t *testing.T) {
	for _, stream := range []bool{false} {
		var mu sync.Mutex
		var order []string
		record := func(s string) {
			mu.Lock()
			order = append(order, s)
			mu.Unlock()
		}
		tool := func(name string) types.Tool {
			return types.Tool{
				Name:       name,
				Parameters: map[string]interface{}{"type": "object"},
				OnInputStart: func(context.Context, types.OnInputStartOptions) error {
					record("start:" + name)
					return nil
				},
				OnInputAvailable: func(context.Context, types.OnInputAvailableOptions) error {
					record("input:" + name)
					return nil
				},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "ok", nil
				},
			}
		}
		_, err := runTextCall(t, stream, GenerateTextOptions{
			Model: failureToolCallModel(
				types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}},
				types.ToolCall{ID: "c2", ToolName: "b", Arguments: map[string]interface{}{}},
			),
			Prompt: "go",
			Tools:  []types.Tool{tool("a"), tool("b")},
			ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
				record("approve:" + o.ToolCall.ToolName)
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
			}),
			StopWhen: []StopCondition{StepCountIs(1)},
		})
		if err != nil {
			t.Fatalf("stream=%v: %v", stream, err)
		}
		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		want := []string{"start:a", "input:a", "approve:a", "start:b", "input:b", "approve:b"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("stream=%v: order = %v, want %v", stream, got, want)
		}
	}
}

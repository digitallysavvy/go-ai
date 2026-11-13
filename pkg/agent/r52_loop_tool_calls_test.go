package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r52ToolCallModel(calls ...types.ToolCall) *testutil.MockLanguageModel {
	var step atomic.Int32
	return &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if step.Add(1) == 1 {
				return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
}

// The legacy loop notifies each valid call's tool and resolves that call's
// approval before the next call (TS generateText's notification loop), and
// an approval that cannot be resolved fails the run before any tool runs,
// even with tool calls running concurrently.
func TestR52LegacyLoopNotifiesAndResolvesApprovalsBeforeTools(t *testing.T) {
	var mu sync.Mutex
	var order []string
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	var executed atomic.Int32
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
				executed.Add(1)
				return "ok", nil
			},
		}
	}
	a := NewToolLoopAgent(AgentConfig{
		Model: r52ToolCallModel(
			types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}},
			types.ToolCall{ID: "c2", ToolName: "b", Arguments: map[string]interface{}{}},
		),
		Tools: []types.Tool{tool("a"), tool("b")},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			record("approve:" + o.ToolCall.ToolName)
			if o.ToolCall.ToolName == "b" {
				return types.ToolApprovalResult{Status: "bogus"}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}),
	})
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err == nil {
		t.Fatal("run succeeded, want the approval resolution error")
	}
	if executed.Load() != 0 {
		t.Fatalf("%d tools executed before the failing approval", executed.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:a", "input:a", "approve:a", "start:b", "input:b", "approve:b"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// The legacy loop's per-step timeout covers the step's tool execution, as
// TS generateText's step timeout aborts the signal executeTools uses; a
// tool that swallows the abort still ends the run with the step timeout.
func TestR52LegacyLoopStepTimeoutCoversTools(t *testing.T) {
	step := 30 * time.Millisecond
	var toolErr atomic.Value
	a := NewToolLoopAgent(AgentConfig{
		Model: r52ToolCallModel(types.ToolCall{ID: "c1", ToolName: "slow", Arguments: map[string]interface{}{}}),
		Tools: []types.Tool{{
			Name:       "slow",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				select {
				case <-ctx.Done():
					toolErr.Store(ctx.Err())
					return "swallowed", nil
				case <-time.After(3 * time.Second):
					return "late", nil
				}
			},
		}},
		Timeout: &ai.TimeoutConfig{PerStep: &step},
	})
	started := time.Now()
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if time.Since(started) > 2*time.Second {
		t.Fatal("the step timeout did not reach the tool")
	}
	if e, _ := toolErr.Load().(error); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("tool ctx err = %v, want the step deadline", e)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the step deadline", err)
	}
}

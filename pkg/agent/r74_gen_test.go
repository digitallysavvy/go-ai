package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r74genAgentIntPtr(v int) *int { return &v }

// The legacy loop fires the structured tool start callbacks in call order, as
// TS executeTools and ai.GenerateText do (r73 coordinator ruling); a call
// that never reaches its start (a client tool without Execute) keeps the
// chain.
func TestR74GenAgentParallelToolStartOrder(t *testing.T) {
	calls := []types.ToolCall{
		{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}},
		{ID: "call-2", ToolName: "client", Arguments: map[string]interface{}{}},
		{ID: "call-3", ToolName: "t", Arguments: map[string]interface{}{}},
		{ID: "call-4", ToolName: "t", Arguments: map[string]interface{}{}},
	}
	for _, concurrency := range []*int{nil, r74genAgentIntPtr(2)} {
		var mu sync.Mutex
		n := 0
		var starts []string
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				mu.Lock()
				n++
				first := n == 1
				mu.Unlock()
				if first {
					return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}, nil
				}
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			},
		}
		a := NewToolLoopAgent(AgentConfig{
			Model:               model,
			ToolCallConcurrency: concurrency,
			Tools: []types.Tool{
				{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "ok", nil
				}},
				{Name: "client", Parameters: map[string]interface{}{"type": "object"}},
			},
			OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
				if e.ToolCall.ID == "call-1" {
					time.Sleep(40 * time.Millisecond)
				}
				mu.Lock()
				defer mu.Unlock()
				starts = append(starts, e.ToolCall.ID)
			},
		})
		if _, err := a.Execute(context.Background(), "go"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := strings.Join(starts, ",")
		mu.Unlock()
		if got != "call-1,call-3,call-4" {
			t.Errorf("concurrency=%v: start order = %s, want call-1,call-3,call-4", concurrency, got)
		}
	}
}

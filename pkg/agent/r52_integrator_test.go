package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy loop resumes history approvals with the prepared call's tools
// (TS generateText resumes them after prepareCall): a tool that only
// PrepareCall supplies runs for an approved request.
func TestR52IntegratorLoopResumeUsesPreparedTools(t *testing.T) {
	var executed atomic.Bool
	lookup := types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed.Store(true)
			return "found", nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			ProviderName: "p", ModelName: "m",
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			},
		},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Tools = []types.Tool{lookup}
			return c
		},
	})
	history := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "find it"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
		}},
	}
	if _, err := a.ExecuteWithMessages(context.Background(), history); err != nil {
		t.Fatalf("ExecuteWithMessages() error = %v", err)
	}
	if !executed.Load() {
		t.Fatal("the approved tool from PrepareCall did not run")
	}
}

// A total timeout set by PrepareCall bounds the run (TS
// `timeout ?? preparedCall.timeout`).
func TestR52IntegratorLoopTotalTimeoutFromPrepareCall(t *testing.T) {
	total := 30 * time.Millisecond
	a := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			ProviderName: "p", ModelName: "m",
			DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(3 * time.Second):
					return &types.GenerateResult{Text: "late", FinishReason: types.FinishReasonStop}, nil
				}
			},
		},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.Timeout = &ai.TimeoutConfig{Total: &total}
			return c
		},
	})
	started := time.Now()
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if time.Since(started) > 2*time.Second {
		t.Fatal("the PrepareCall total timeout did not bound the run")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the total deadline", err)
	}
}

package workflow

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS WorkflowAgent runs a step's tools with mapWithConcurrency and writes
// their results with writeToolResults in call order once every call settled
// (workflow-agent.ts 3041-3150), unlike streamText's executeToolsFromStream
// (completion order). A fast second call must not stream before the slow
// first one.
func TestR86integWorkflowToolResultsInCallOrder(t *testing.T) {
	var calls atomic.Int32
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		if calls.Add(1) == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "a", ToolName: "slow", RawArguments: "{}"}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "b", ToolName: "fast", RawArguments: "{}"}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	release := make(chan struct{})
	w, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: []types.Tool{
		{Name: "slow", Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			// Settle well after the fast call's end notification.
			time.Sleep(50 * time.Millisecond)
			return "S", nil
		}},
		{Name: "fast", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			defer close(release)
			return "F", nil
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	fs := res.FullStream()
	for {
		c, err := fs.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult {
			parts = append(parts, c.ToolResult.ToolCallID)
		}
	}
	if got := strings.Join(parts, " "); got != "a b" {
		t.Errorf("tool-result parts = %q, want %q", got, "a b")
	}
}

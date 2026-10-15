package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS model-call-iterator.ts: a step whose stream carried an error part
// (hasTerminalError, e.g. a chunk that failed schema validation) never
// processes its tool calls and ends the run, even when the finish part says
// tool-calls or stop. onError fires once, onAbort never.
func TestWorkflowStreamErrorPartSkipsToolCalls(t *testing.T) {
	for _, fr := range []types.FinishReason{types.FinishReasonToolCalls, types.FinishReasonStop} {
		t.Run(string(fr), func(t *testing.T) {
			var mu sync.Mutex
			executed, errs, aborts := 0, 0, 0
			tool := types.Tool{Name: "t1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				executed++
				mu.Unlock()
				return "ok", nil
			}}
			m := &fnModel{chunks: []provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "t"},
				{Type: provider.ChunkTypeText, ID: "t", Text: "hi"},
				{Type: provider.ChunkTypeTextEnd, ID: "t"},
				{Type: provider.ChunkTypeError, Text: "Type validation failed: bad frame"},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "t1", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: fr},
			}}
			a, _ := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{tool}})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			st, err := a.StreamWithOptions(ctx, WorkflowStreamOptions{
				Prompt:  "x",
				OnError: func(context.Context, error) { mu.Lock(); errs++; mu.Unlock() },
				OnAbort: func(context.Context, []types.StepResult) { mu.Lock(); aborts++; mu.Unlock() },
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = st.ConsumeStream()
			mu.Lock()
			defer mu.Unlock()
			if executed != 0 || errs != 1 || aborts != 0 || len(st.Steps()) != 1 || st.Err() == nil {
				t.Fatalf("executed=%d errs=%d aborts=%d steps=%d err=%v", executed, errs, aborts, len(st.Steps()), st.Err())
			}
		})
	}
}

package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS notify swallows a throwing onToolExecutionEnd, and executeToolsFromStream
// still enqueues the settled result right after it. Recorded with streamText
// under Node v24 (packages/ai dist at 30548ee7f9), onToolExecutionEnd throws:
// tool-call:a tool-call:b tool-result:b tool-result:a. Go's panicking
// OnToolExecutionEnd / OnToolCallFinish must not hold the live result back.
func TestR86bstreamPanickingToolEndKeepsLiveResults(t *testing.T) {
	for _, deprecated := range []bool{false, true} {
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "a", ToolName: "slow", RawArguments: "{}"}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "b", ToolName: "fast", RawArguments: "{}"}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
			}), nil
		}}
		release := make(chan struct{})
		boom := func(context.Context, OnToolCallFinishEvent) { panic("boom") }
		opts := StreamTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{
			{Name: "slow", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				select {
				case <-release:
				case <-time.After(300 * time.Millisecond):
				}
				return "S", nil
			}},
			{Name: "fast", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				go func() { time.Sleep(50 * time.Millisecond); close(release) }()
				return "F", nil
			}},
		}}
		if deprecated {
			opts.OnToolCallFinish = boom //nolint:staticcheck // the deprecated fallback takes the same path
		} else {
			opts.OnToolExecutionEnd = boom
		}
		r, err := StreamText(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		fs := r.FullStream()
		for {
			c, err := fs.Next()
			if err != nil {
				break
			}
			switch c.Type {
			case provider.ChunkTypeToolCall:
				parts = append(parts, "tool-call:"+c.ToolCall.ID)
			case provider.ChunkTypeToolResult:
				parts = append(parts, "tool-result:"+c.ToolResult.ToolCallID)
			case provider.ChunkTypeError:
				parts = append(parts, "error")
			}
		}
		r86streamWantStrings(t, "parts", parts, "tool-call:a", "tool-call:b", "tool-result:b", "tool-result:a")
	}
}

package ai

import (
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Reviewer item 4 (r79): the inline invalid tool errors reset at a stream
// retry boundary. TS stream-text.ts flushes the held tool parts at
// model-call-end and returns; an error part read after it (no tool parts
// held) is retried while the budget remains, and the step's recorded content
// restarts at the attempt boundary. So an invalid call answered in the first
// attempt must leave nothing in the step that the retry produced.
func TestR79StreamRetryDropsInlineInvalidToolError(t *testing.T) {
	retries := 1
	first := []provider.StreamChunk{
		r78streamUnknownCall("call-1"),
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r72streamTestUsage},
		{Type: provider.ChunkTypeError, Text: "Internal Server Error"},
	}
	r, parts := r73streamParts(t, StreamTextOptions{
		Model:         r71streamModel(nil, first, r71streamHelloWorld),
		Prompt:        "p",
		Tools:         []types.Tool{r71streamTool1(true, nil, nil)},
		StreamRetries: &retries,
	})
	// The first attempt's call and its tool-error were already forwarded
	// (TS flushes them at model-call-end, before the error part).
	if got, want := fmt.Sprint(r73streamNames(parts)), "[start start-step tool-call:call-1:unknown tool-result:call-1:<nil> text-start:1 text-delta:1:Hello text-delta:1:,  text-delta:1:world! text-end:1 finish-step:stop finish:stop]"; got != want {
		t.Fatalf("full stream = %s\nwant          %s", got, want)
	}
	steps := r.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	step := steps[0]
	if got := fmt.Sprint(r78streamContentKinds(step.Content)); got != "[types.TextContent]" {
		t.Fatalf("step content = %s, want [types.TextContent]", got)
	}
	if len(step.ToolCalls) != 0 || len(step.ToolResults) != 0 {
		t.Fatalf("step tool calls = %+v, tool results = %+v, want none", step.ToolCalls, step.ToolResults)
	}
	for _, m := range step.Response.Messages {
		if m.Role == types.RoleTool {
			t.Fatalf("response has a tool message: %+v", m)
		}
	}
	if r.FinishReason() != types.FinishReasonStop {
		t.Fatalf("finish reason = %s, want stop", r.FinishReason())
	}
}

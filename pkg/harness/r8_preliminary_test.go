package harness

import (
	"context"
	"iter"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS maybeExecuteHostTool passes onPreliminaryResult: every value a
// generator-form host tool yields becomes a preliminary
// tool-result part on the consumer stream (work dir stripped); only the final
// output is submitted to the runtime and recorded in the step.
func TestRunPrompt_GeneratorHostToolStreamsPreliminaryResults(t *testing.T) {
	tool := types.Tool{
		Name: "scan", Parameters: map[string]interface{}{"type": "object"},
		Execute: types.StreamingExecutor(func(ctx context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
			return func(yield func(interface{}, error) bool) {
				if !yield(map[string]any{"status": "loading", "path": "/work/dir/a.txt"}, nil) {
					return
				}
				yield(map[string]any{"status": "done"}, nil)
			}
		}),
	}
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "call", ToolName: "scan", Input: "{}"})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			emit(&ToolResultPart{ToolCallID: "call", ToolName: "scan", Result: map[string]any{"status": "done"}})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: stop, TotalUsage: stringUsage(2, 2)})
		},
	)
	tools := map[string]types.Tool{"scan": tool}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("Scan"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools, SessionWorkDir: "/work/dir",
	})
	chunks := drainRunPrompt(t, out)

	var prelim, final int
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolResult) {
		if c.ToolResult.Preliminary {
			prelim++
			m, _ := c.ToolResult.Result.(map[string]any)
			if prelim == 1 && (m["status"] != "loading" || m["path"] != "a.txt") {
				t.Fatalf("preliminary result = %+v, want loading with work dir stripped", c.ToolResult.Result)
			}
		} else {
			final++
		}
	}
	// Each yield is preliminary (TS), the last one is re-emitted as final.
	if prelim != 2 || final != 1 {
		t.Fatalf("preliminary=%d final=%d tool-result chunks, want 2/1", prelim, final)
	}
	if len(mock.toolResults) != 1 {
		t.Fatalf("runtime submissions = %+v, want only the final output", mock.toolResults)
	}
	// TS HarnessStreamTextResult records every tool-result part in step
	// content, preliminary ones included: 2 preliminary + 1 final.
	n, prelimContent, toolMsgParts := 0, 0, 0
	for _, s := range out.Result.Steps() {
		n += len(s.ToolResults)
		for _, c := range s.Content {
			if tr, ok := c.(types.ToolResultContent); ok && tr.Preliminary {
				prelimContent++
			}
		}
		for _, m := range s.ResponseMessages {
			if m.Role == types.RoleTool {
				toolMsgParts += len(m.Content)
			}
		}
	}
	if n != 3 || prelimContent != 2 {
		t.Fatalf("step tool results = %d (preliminary content %d), want 3 (2)", n, prelimContent)
	}
	if toolMsgParts != 3 {
		t.Fatalf("tool message parts = %d, want 3 (TS toResponseMessages emits one per recorded result)", toolMsgParts)
	}
}

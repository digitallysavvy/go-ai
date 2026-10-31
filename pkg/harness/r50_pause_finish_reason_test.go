package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A turn that pauses for host input ends with TS `result.finish()` (no
// input): the finish part and the result report the last completed step's
// finish reason ('tool-calls' for the step closed at the pause boundary), not
// an empty one.
func TestR50HostInputPauseFinishReasonIsLastStepReason(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "host-input"}, Usage: stringUsage(7, 0)},
			}
		},
	})
	tools := map[string]types.Tool{"weather": weatherTool()}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval: ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
	})
	chunks := drainRunPrompt(t, out)
	finishes := chunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 || finishes[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish chunks = %+v, want one with tool-calls", finishes)
	}
	if got := out.Result.FinishReason(); got != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason() = %q, want tool-calls", got)
	}
}

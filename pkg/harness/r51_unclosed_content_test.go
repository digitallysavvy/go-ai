package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS HarnessStreamTextResult.finish() fails a turn only when the open step
// recorded content (currentStepContent: text, reasoning, tool calls, results,
// approvals). A step that saw only tool-input parts before the terminal
// finish has none, so the turn finishes normally; with text it fails.
func TestR51UnclosedStepFailsOnlyWithContent(t *testing.T) {
	for _, withText := range []bool{false, true} {
		mock := newMockHarness(mockHarnessOptions{
			script: func(submit func(string, interface{})) []StreamPart {
				parts := []StreamPart{
					&StreamStartPart{},
					&ToolInputStartPart{ID: "c1", ToolName: "weather"},
					&ToolInputDeltaPart{ID: "c1", Delta: "{"},
				}
				if withText {
					parts = append(parts, &TextDeltaPart{ID: "t1", Delta: "hi"})
				}
				return append(parts, &FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
			},
		})
		out := runPrompt(context.Background(), runPromptInput{
			Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		})
		chunks := drainRunPrompt(t, out)
		errs := chunksOfType(chunks, provider.ChunkTypeError)
		if withText {
			if len(errs) != 1 || errs[0].Text != unclosedStepErrorMessage {
				t.Fatalf("with text: error chunks = %+v", errs)
			}
			continue
		}
		if len(errs) != 0 || out.Result.Err() != nil {
			t.Fatalf("tool input only: error chunks = %+v, err = %v", errs, out.Result.Err())
		}
		if f := chunksOfType(chunks, provider.ChunkTypeFinish); len(f) != 1 {
			t.Fatalf("tool input only: finish chunks = %+v", f)
		}
	}
}

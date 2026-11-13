package harness

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// A turn stopped by a stop condition ends with TS result.finish() without
// input: the finish part carries the stop-boundary step's finish reason, the
// last step's provider metadata and the usage summed over every step
// (accumulatedUsage), not only the last step's.
func TestR51IntegratorStopConditionFinishSumsUsage(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "one"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "tool_use"}, Usage: stringUsage(7, 2), HarnessMetadata: Metadata{"x": {"step": 1}}},
				&TextStartPart{ID: "t2"}, &TextDeltaPart{ID: "t2", Delta: "two"}, &TextEndPart{ID: "t2"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "tool_use"}, Usage: stringUsage(5, 3), HarnessMetadata: Metadata{"x": {"step": 2}}},
				&TextStartPart{ID: "t3"},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(100, 100)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		StopConditions: []ai.StopCondition{ai.IsStepCount(2)},
	})
	finishes := chunksOfType(drainRunPrompt(t, out), provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("%d finish chunks", len(finishes))
	}
	f := finishes[0]
	if f.Usage == nil || f.Usage.InputTokens == nil || f.Usage.OutputTokens == nil || *f.Usage.InputTokens != 12 || *f.Usage.OutputTokens != 5 {
		t.Errorf("finish usage = %+v, want 12 in / 5 out (summed steps)", f.Usage)
	}
	if string(f.ProviderMetadata) != `{"x":{"step":2}}` || f.RawFinishReason != "tool_use" {
		t.Errorf("finish metadata = %s, raw = %q", f.ProviderMetadata, f.RawFinishReason)
	}
}

// TS appendToCurrentStepContent records text-start/-delta, reasoning-start/
// -delta, tool-call, tool-result, tool-error and approval parts; finish()
// fails the turn only when the open step holds one of them. Each kind alone
// fails the turn (with a terminal finish and with a stream that just ends;
// an approval request needs its tool call, which already counts);
// tool-input, text-end and reasoning-end parts alone do not.
func TestR51IntegratorUnclosedStepContentKinds(t *testing.T) {
	cases := []struct {
		name  string
		parts []StreamPart
		fails bool
	}{
		{"text-start", []StreamPart{&TextStartPart{ID: "t1"}}, true},
		{"text-delta", []StreamPart{&TextDeltaPart{ID: "t1", Delta: "x"}}, true},
		{"reasoning-start", []StreamPart{&ReasoningStartPart{ID: "r1"}}, true},
		{"reasoning-delta", []StreamPart{&ReasoningDeltaPart{ID: "r1", Delta: "x"}}, true},
		{"tool-call", []StreamPart{&ToolCallPart{ToolCallID: "c1", ToolName: "web_search", Input: "{}", ProviderExecuted: true}}, true},
		{"tool-result", []StreamPart{&ToolResultPart{ToolCallID: "c1", ToolName: "web_search", Result: "r"}}, true},
		{"tool-input only", []StreamPart{&ToolInputStartPart{ID: "c1", ToolName: "weather"}, &ToolInputDeltaPart{ID: "c1", Delta: "{"}, &ToolInputEndPart{ID: "c1"}}, false},
		{"text-end only", []StreamPart{&TextEndPart{ID: "t1"}}, false},
		{"reasoning-end only", []StreamPart{&ReasoningEndPart{ID: "r1"}}, false},
	}
	for _, tc := range cases {
		for _, terminal := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/terminal=%v", tc.name, terminal), func(t *testing.T) {
				mock := newMockHarness(mockHarnessOptions{
					script: func(submit func(string, interface{})) []StreamPart {
						parts := append([]StreamPart{&StreamStartPart{}}, tc.parts...)
						if terminal {
							parts = append(parts, &FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
						}
						return parts
					},
				})
				out := runPrompt(context.Background(), runPromptInput{
					Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
				})
				chunks := drainRunPrompt(t, out)
				unclosed := false
				for _, e := range chunksOfType(chunks, provider.ChunkTypeError) {
					if e.Text == unclosedStepErrorMessage || (e.Err != nil && e.Err.Error() == unclosedStepErrorMessage) {
						unclosed = true
					}
				}
				if err := out.Result.Err(); err != nil && err.Error() == unclosedStepErrorMessage {
					unclosed = true
				}
				if unclosed != tc.fails {
					t.Fatalf("unclosed-step failure = %v, want %v (chunks %+v)", unclosed, tc.fails, chunks)
				}
				// The harness fails the turn itself (d.fail), which keeps the partial
				// step, as TestAgent_UnclosedStepIsProtocolError pins for text; the
				// pkg/ai backstop for a finish over unclosed content records none.
				wantSteps := 0
				if tc.fails {
					wantSteps = 1
				}
				if n := len(out.Result.Steps()); n != wantSteps {
					t.Fatalf("steps = %d, want %d", n, wantSteps)
				}
			})
		}
	}
}

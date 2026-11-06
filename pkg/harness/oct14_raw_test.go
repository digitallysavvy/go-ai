package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func rawMarkers(chunks []provider.StreamChunk) []interface{} {
	var out []interface{}
	for _, c := range chunksOfType(chunks, provider.ChunkTypeRaw) {
		out = append(out, c.Raw)
	}
	return out
}

// Ports TS #22258 "passes raw parts through without opening a step or adding
// to one". A raw part between the last finish-step and the terminal finish
// must not open a step (which the terminal finish would then reject as
// "unclosed step content") nor start a step before the next model output.
func TestRunPrompt_RawPartDoesNotOpenStep(t *testing.T) {
	var starts int
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "one"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&RawPart{RawValue: map[string]any{"marker": "between-steps"}},
				&TextStartPart{ID: "t2"}, &TextDeltaPart{ID: "t2", Delta: "two"}, &TextEndPart{ID: "t2"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&RawPart{RawValue: map[string]any{"marker": "before-finish"}},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Callbacks: Callbacks{OnStepStart: func(ctx context.Context, e ai.OnStepStartEvent) { starts++ }},
	})
	chunks := drainRunPrompt(t, out)
	if err := out.Result.Err(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if got := rawMarkers(chunks); len(got) != 2 {
		t.Fatalf("raw chunks = %+v, want 2", got)
	}
	if n := len(out.Result.Steps()); n != 2 {
		t.Fatalf("steps = %d, want 2", n)
	}
	if starts != 2 {
		t.Fatalf("OnStepStart fired %d times, want 2 (raw must not open a step)", starts)
	}
}

// Ports TS #22258 "forwards a raw part that arrives right after the step a
// stop condition ends on".
func TestRunPrompt_RawPartAtStopBoundaryIsForwarded(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "one"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&RawPart{RawValue: map[string]any{"marker": "usage-after-step"}},
				&TextDeltaPart{ID: "t", Delta: "ignored"},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		StopConditions: []ai.StopCondition{func(s ai.StopConditionState) string {
			if len(s.Steps) == 1 {
				return "one-step"
			}
			return ""
		}},
	})
	chunks := drainRunPrompt(t, out)
	got := rawMarkers(chunks)
	if len(got) != 1 {
		t.Fatalf("raw chunks = %+v, want exactly the usage-after-step marker", got)
	}
	if m, _ := got[0].(map[string]any); m["marker"] != "usage-after-step" {
		t.Fatalf("raw = %+v", got[0])
	}
}

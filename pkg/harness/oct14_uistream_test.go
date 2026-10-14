package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// Ports TS #23081: step callbacks fire from the harness result's UI stream
// helpers. In Go the harness result is a *ai.StreamTextResult, so
// UIMessageStreamResultOptions.OnStepEnd / deprecated OnStepFinish apply.
func TestRunPrompt_UIStreamStepCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name               string
		useEnd, useFinish  bool
		wantEnd, wantFinal int
	}{
		{"OnStepEnd", true, false, 2, 0},
		{"deprecated OnStepFinish", false, true, 0, 2},
		{"prefers OnStepEnd", true, true, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockHarness(mockHarnessOptions{
				script: func(submit func(string, interface{})) []StreamPart {
					return []StreamPart{
						&StreamStartPart{},
						&TextStartPart{ID: "first"}, &TextDeltaPart{ID: "first", Delta: "First"}, &TextEndPart{ID: "first"},
						&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
						&TextStartPart{ID: "second"}, &TextDeltaPart{ID: "second", Delta: "Second"}, &TextEndPart{ID: "second"},
						&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
						&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
					}
				},
			})
			out := runPrompt(context.Background(), runPromptInput{
				Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
			})
			var mu sync.Mutex
			var ends, finishes int
			opts := ai.UIMessageStreamResultOptions{}
			if tc.useEnd {
				opts.OnStepEnd = func(map[string]interface{}) { mu.Lock(); ends++; mu.Unlock() }
			}
			if tc.useFinish {
				opts.OnStepFinish = func(map[string]interface{}) { mu.Lock(); finishes++; mu.Unlock() }
			}
			chunks, errs := out.Result.ToUIMessageStream(context.Background(), opts)
			finishSteps := 0
			for c := range chunks {
				if c["type"] == "finish-step" {
					finishSteps++
				}
			}
			if err := <-errs; err != nil {
				t.Fatalf("ui stream error: %v", err)
			}
			<-out.Done
			mu.Lock()
			defer mu.Unlock()
			if finishSteps != 2 || ends != tc.wantEnd || finishes != tc.wantFinal {
				t.Fatalf("finish-step=%d ends=%d finishes=%d, want 2/%d/%d", finishSteps, ends, finishes, tc.wantEnd, tc.wantFinal)
			}
		})
	}
}

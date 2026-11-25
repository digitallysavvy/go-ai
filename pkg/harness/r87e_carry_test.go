package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS run-prompt.ts sets execution.toolOutput from a call's first runtime
// result only and pushes execution.toolOutput into stepProviderToolResults on
// every result of a provider-executed call, so a preliminary result followed
// by the final one puts the preliminary outcome into the model-call content
// twice.
func TestR87ecarryProviderPreliminaryResultRepeatsInModelCallContent(t *testing.T) {
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "n1", ToolName: "Read", Input: `{}`, ProviderExecuted: true, Dynamic: true})
			emit(&ToolResultPart{ToolCallID: "n1", ToolName: "Read", Result: "partial", Preliminary: true, Dynamic: true})
			emit(&ToolResultPart{ToolCallID: "n1", ToolName: "Read", Result: "final", Dynamic: true})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
		nil,
	)
	var mu sync.Mutex
	var callContent []types.ContentPart
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Callbacks: Callbacks{
			OnLanguageModelCallEnd: func(_ context.Context, e ai.LanguageModelCallEndEvent) {
				mu.Lock()
				callContent = e.Content
				mu.Unlock()
			},
		},
	})
	drainRunPrompt(t, out)
	if err := out.Result.Err(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	var results []interface{}
	for _, p := range callContent {
		if r, ok := p.(types.ToolResultContent); ok {
			results = append(results, r.Result)
		}
	}
	if len(results) != 2 || results[0] != "partial" || results[1] != "partial" {
		t.Fatalf("model-call content results = %#v, want the first outcome twice (TS)", results)
	}
}

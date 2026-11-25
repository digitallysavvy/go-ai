package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS records a runtime tool-result's outcome from the unstripped event
// (`execution.toolOutput = { ..., output: value.result }`), so the
// tool-execution-end callback and the model-call-end content keep the
// absolute work-dir path; only the consumer stream sees the stripped copy.
func TestR47ToolExecutionEndKeepsUnstrippedRuntimeResult(t *testing.T) {
	const workDir = "/work/session-1"
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "n1", ToolName: "Read", Input: `{}`, ProviderExecuted: true, Dynamic: true})
			emit(&ToolResultPart{ToolCallID: "n1", ToolName: "Read", Result: workDir + "/main.go", Dynamic: true})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
		nil,
	)
	var mu sync.Mutex
	var endOutput interface{}
	var callContent []types.ContentPart
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), SandboxSession: testSandbox(), SessionWorkDir: workDir,
		Callbacks: Callbacks{
			OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
				mu.Lock()
				endOutput = e.ToolOutput.Result
				mu.Unlock()
			},
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
	if endOutput != workDir+"/main.go" {
		t.Fatalf("OnToolExecutionEnd output = %#v, want the unstripped path", endOutput)
	}
	var got interface{}
	for _, p := range callContent {
		if r, ok := p.(types.ToolResultContent); ok {
			got = r.Result
		}
	}
	if got != workDir+"/main.go" {
		t.Fatalf("model-call content result = %#v (content %+v)", got, callContent)
	}
	steps := out.Result.Steps()
	if len(steps) != 1 || len(steps[0].ToolResults) != 1 || steps[0].ToolResults[0].Result == workDir+"/main.go" {
		t.Fatalf("step tool results = %+v, want the stripped display copy", steps)
	}
}

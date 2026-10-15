package harness

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A step's `harnessMetadata` on finish-step and the turn's on finish reach the
// step's ProviderMetadata and the result (TS completeStep providerMetadata).
func TestRunPrompt_FinishStepHarnessMetadataReachesStep(t *testing.T) {
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t"}, &TextDeltaPart{ID: "t", Delta: "hi"}, &TextEndPart{ID: "t"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1), HarnessMetadata: Metadata{"mock": {"k": "v"}}},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
	})
	drainRunPrompt(t, out)
	steps := out.Result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	md, _ := steps[0].ProviderMetadata["mock"].(map[string]any)
	if md["k"] != "v" {
		t.Fatalf("step ProviderMetadata = %+v", steps[0].ProviderMetadata)
	}
}

// A failing host tool reports `String(err)` ("Error: msg") to the runtime.
func TestRunPrompt_HostToolErrorUsesJSErrorString(t *testing.T) {
	var mu sync.Mutex
	var got []ToolResultSubmission
	boom := weatherTool()
	boom.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return nil, errors.New("boom")
	}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{}`})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			mu.Lock()
			got = append(got, sub)
			mu.Unlock()
			emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: "weather", Result: sub.Output, IsError: sub.IsError})
			stop := FinishReason{Unified: FinishReasonStop}
			emit(&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)})
		},
	)
	tools := map[string]types.Tool{"weather": boom}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	drainRunPrompt(t, out)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !got[0].IsError {
		t.Fatalf("submissions = %+v", got)
	}
	if m, _ := got[0].Output.(map[string]interface{}); m["error"] != "Error: boom" {
		t.Fatalf("output = %+v", got[0].Output)
	}
}

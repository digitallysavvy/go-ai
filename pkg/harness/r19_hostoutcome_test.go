package harness

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A host tool whose context fails validation reports a tool error and the
// turn goes on (TS maybeExecuteHostTool returns executed:true, ok:false); the
// detailed error (which can carry credentials) reaches only the host-side
// callback, never the runtime.
func TestRunPrompt_ContextValidationFailureIsToolErrorNotTurnFailure(t *testing.T) {
	executed := false
	tool := types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"userId": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"userId"},
		}),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return nil, nil
		},
	}
	// The runtime echoes a host result only after it was submitted.
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "lookup", Input: `{}`})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: "lookup", IsError: sub.IsError, Result: sub.Output})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
	)
	var mu sync.Mutex
	var endErr error
	tools := map[string]types.Tool{"lookup": tool}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(),
		ToolsContext:   map[string]interface{}{"lookup": map[string]interface{}{"userId": 123, "apiKey": "host-secret"}},
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			endErr = e.ToolOutput.Error
			mu.Unlock()
		}},
	})
	drainRunPrompt(t, out)

	if err := out.Result.Err(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if executed {
		t.Fatal("tool executed despite invalid context")
	}
	if len(mock.toolResults) != 1 || !mock.toolResults[0].IsError {
		t.Fatalf("submitted = %+v, want one error result", mock.toolResults)
	}
	if m, _ := mock.toolResults[0].Output.(map[string]interface{}); m["error"] != "Tool context validation failed." {
		t.Fatalf("submitted output = %+v", mock.toolResults[0].Output)
	}
	mu.Lock()
	defer mu.Unlock()
	if endErr == nil || !strings.Contains(endErr.Error(), "host-secret") {
		t.Fatalf("OnToolExecutionEnd error = %v, want the detailed host-side validation error", endErr)
	}
}

// An approved custom tool runs inline on the continuation path; its result
// is surfaced as a tool-result part (the runtime's echo of a settled call is
// skipped) and reported to OnToolExecutionEnd with the raw output.
func TestRunPrompt_ApprovedContinuationSurfacesToolResult(t *testing.T) {
	const workDir = "/work/dir"
	tool := types.Tool{
		Name: "weather",
		Parameters: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return workDir + "/out.txt", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var endOut interface{}
	tools := map[string]types.Tool{"weather": tool}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue",
		Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(), SessionWorkDir: workDir,
		PendingToolApprovals: []PendingToolApproval{
			{ApprovalID: "a1", ToolCallID: "c1", ToolName: "weather", Input: `{"city":"Paris"}`, Kind: PendingToolApprovalCustom},
		},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}},
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			endOut = e.ToolOutput.Result
			mu.Unlock()
		}},
	})
	chunks := drainRunPrompt(t, out)

	results := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(results) != 1 || results[0].ToolResult.Error != nil {
		t.Fatalf("tool-result chunks = %+v, want one success", results)
	}
	if got := results[0].ToolResult.Result; got != "out.txt" {
		t.Fatalf("surfaced result = %v, want work-dir-stripped %q", got, "out.txt")
	}
	mu.Lock()
	defer mu.Unlock()
	if endOut != workDir+"/out.txt" {
		t.Fatalf("OnToolExecutionEnd result = %v, want the raw output", endOut)
	}
}

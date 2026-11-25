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

func portcarryharContextTool(executed *bool) types.Tool {
	return types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"userId": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"userId"},
		}),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			*executed = true
			return nil, nil
		},
	}
}

// TS 1a94af38c7 "keeps host tool context out of the stream when an approved
// host tool fails context validation": the tool-error part carries the error
// the runtime received, the callback keeps the detailed one.
func TestPortcarryharApprovedContextValidationErrorStaysOutOfStream(t *testing.T) {
	executed := false
	tools := map[string]types.Tool{"lookup": portcarryharContextTool(&executed)}
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var endErr error
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue",
		Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
		ToolsContext: map[string]interface{}{"lookup": map[string]interface{}{"userId": 123, "apiKey": "host-secret"}},
		PendingToolApprovals: []PendingToolApproval{
			{ApprovalID: "a1", ToolCallID: "c1", ToolName: "lookup", Input: `{}`, Kind: PendingToolApprovalCustom},
		},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}},
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			mu.Lock()
			endErr = e.ToolOutput.Error
			mu.Unlock()
		}},
	})
	chunks := drainRunPrompt(t, out)
	if executed {
		t.Fatal("tool executed despite invalid context")
	}
	results := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(results) != 1 || results[0].ToolResult.Error == nil {
		t.Fatalf("tool-result chunks = %+v, want one tool-error", results)
	}
	if got := results[0].ToolResult.Error.Error(); got != "Tool context validation failed." {
		t.Fatalf("stream error = %q, want the text the runtime received", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if endErr == nil || !strings.Contains(endErr.Error(), "host-secret") {
		t.Fatalf("OnToolExecutionEnd error = %v, want the detailed host-side error", endErr)
	}
}

// A tool that panics with a value that is not an error or a string reaches
// the stream as the text the runtime received, never as the value (TS
// "sends the text that the runtime received when a host tool throws an
// object"). The turn pauses for a client tool, so the harness itself
// reports the host tool outcome.
func TestPortcarryharPanicValueStaysOutOfStream(t *testing.T) {
	tool := types.Tool{
		Name: "failing", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			panic(struct {
				Status int
				Body   string
			}{500, "host-secret"})
		},
	}
	askUser := types.Tool{Name: "askUser", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
	tools := map[string]types.Tool{"failing": tool, "askUser": askUser}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "failing", Input: `{}`})
			emit(&ToolCallPart{ToolCallID: "c2", ToolName: "askUser", Input: `{}`})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, TotalUsage: stringUsage(1, 1)})
		},
		nil,
	)
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
	})
	chunks := drainRunPrompt(t, out)
	var failed *types.ToolResult
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolResult) {
		if c.ToolResult.ToolCallID == "c1" {
			failed = c.ToolResult
		}
	}
	if failed == nil || failed.Error == nil {
		t.Fatalf("no tool-error for c1 in %d chunks", len(chunks))
	}
	submitted, _ := mock.toolResults[0].Output.(map[string]interface{})
	if got := failed.Error.Error(); "Error: "+got != submitted["error"] {
		t.Fatalf("stream error %q differs from the submitted text %v", got, submitted["error"])
	}
}

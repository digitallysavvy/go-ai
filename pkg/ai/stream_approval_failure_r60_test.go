package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS execute-tools-from-stream resolves the approval on the tool-call part
// itself; a resolveToolApproval throw errors the transform (it is outside the
// try that turns tool execution failures into `error` parts), so the stream
// errors there: no later model parts, no finish-step, and onError (which sees
// only error parts) is not called. Go resolves the approval eagerly on the
// tool-call chunk too, so the approval branch of executeToolsConfigured is
// never reached from StreamText.
func TestR60StreamTextApprovalFailureErrorsTheStreamWithoutOnError(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "after the call"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}}
	var mu sync.Mutex
	var onErrors, stepErrors, executed int
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt: "a",
		Model:  model,
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			executed++
			mu.Unlock()
			return "r", nil
		}}},
		ToolApproval: types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: "bogus"}
		}),
		OnError:     func(context.Context, error) { mu.Lock(); onErrors++; mu.Unlock() },
		OnStepError: func(context.Context, OnStepErrorEvent) { mu.Lock(); stepErrors++; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := r58FullStreamParts(t, res)
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "unknown tool approval status") {
		t.Fatalf("Err() = %v, want the approval resolution error", res.Err())
	}
	for _, unwanted := range []string{"text-delta", "finish-step", "tool-approval-request", " finish"} {
		if strings.Contains(" "+parts, unwanted) {
			t.Fatalf("parts %q contain %q; TS errors the stream at the tool-call part", parts, unwanted)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if onErrors != 0 || stepErrors != 1 || executed != 0 {
		t.Fatalf("OnError = %d, OnStepError = %d, executed = %d; want 0, 1, 0", onErrors, stepErrors, executed)
	}
}

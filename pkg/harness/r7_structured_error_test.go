package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A failed provider-executed tool-result keeps its structured error value (TS
// tool-error.error is the raw event.result; the UI chunk JSON-stringifies a
// non-string error). Core carries it in ToolResult.Result next to Error.
func TestTranslatePart_StructuredProviderExecutedErrorKeepsValue(t *testing.T) {
	payload := map[string]any{"code": "E1", "retryable": true}
	chunks := TranslatePart(&ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: payload, IsError: true},
		TranslateOptions{IsProviderExecuted: func(string) bool { return true }})
	if len(chunks) != 1 || chunks[0].ToolResult == nil {
		t.Fatalf("chunks = %+v", chunks)
	}
	tr := chunks[0].ToolResult
	if tr.Error == nil || tr.Result == nil {
		t.Fatalf("Error/Result = %v / %v, want both set", tr.Error, tr.Result)
	}

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "bash", ProviderExecuted: true, Dynamic: true}},
		chunks[0],
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})
	res := ai.NewStreamTextResultFromParts(context.Background(), src, ai.ExternalStreamOptions{Provider: "harness", ModelID: "m"})
	uiCh, errCh := ai.ToUIMessageStream(context.Background(), res.Stream())
	var got string
	for c := range uiCh {
		if c["type"] == "tool-output-error" {
			got, _ = c["errorText"].(string)
		}
	}
	<-errCh
	want, _ := json.Marshal(payload)
	if got != string(want) {
		t.Fatalf("errorText = %q, want %q", got, want)
	}
}

// The tool-execution-end event's output carries dynamic like the call (TS
// tool-result.dynamic) and a provider-executed error keeps its raw value.
func TestRunPrompt_ToolExecutionEndOutputCarriesDynamicAndRawError(t *testing.T) {
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "n1", ToolName: "nativeLookup", Input: `{}`, ProviderExecuted: true, Dynamic: true})
			emit(&ToolResultPart{ToolCallID: "n1", ToolName: "nativeLookup", Result: map[string]any{"code": "E1"}, IsError: true})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {},
	)
	var got ai.OnToolCallFinishEvent
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Callbacks: Callbacks{OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) { got = e }},
	})
	drainRunPrompt(t, out)
	if !got.ToolOutput.Dynamic || got.ToolOutput.Result == nil {
		t.Fatalf("ToolOutput = %+v, want Dynamic and raw Result", got.ToolOutput)
	}
}

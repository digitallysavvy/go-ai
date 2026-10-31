package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 72 (stream): ports of TS stream-text.test.ts "... in the ui message
// stream" cases with full-sequence assertions. Package-level identifiers are
// prefixed r72stream.

// r72streamUI runs StreamText and returns its UI message stream chunks as
// JSON-normalized maps (and the first stream error).
func r72streamUI(t *testing.T, opts StreamTextOptions, uiOpts ...UIMessageStreamResultOptions) ([]map[string]interface{}, error) {
	t.Helper()
	if opts.OnError == nil {
		opts.OnError = func(context.Context, error) {}
	}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	ch, errs := r.ToUIMessageStream(context.Background(), uiOpts...)
	var out []map[string]interface{}
	for c := range ch {
		b, mErr := json.Marshal(c)
		if mErr != nil {
			t.Fatal(mErr)
		}
		var m map[string]interface{}
		if uErr := json.Unmarshal(b, &m); uErr != nil {
			t.Fatal(uErr)
		}
		out = append(out, m)
	}
	var first error
	for e := range errs {
		if first == nil {
			first = e
		}
	}
	return out, first
}

func r72streamAssertUI(t *testing.T, opts StreamTextOptions, want string, uiOpts ...UIMessageStreamResultOptions) {
	t.Helper()
	got, err := r72streamUI(t, opts, uiOpts...)
	if err != nil {
		t.Fatalf("ui stream error: %v", err)
	}
	r71streamAssertJSON(t, "ui chunks", got, want)
}

var r72streamTestUsage = &types.Usage{InputTokens: r71streamInt64(3), OutputTokens: r71streamInt64(10), TotalTokens: r71streamInt64(13)}

func r72streamPMD(itemID string) map[string]interface{} {
	return map[string]interface{}{"provider": map[string]interface{}{"itemId": itemID}}
}

// TS 18887: provider-executed tools > single provider-executed tool call and
// result > should include provider-executed tool call and result in the ui
// message stream.
func r72streamProviderExecutedSteps() []provider.StreamChunk {
	isErr := true
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_search", ProviderExecuted: true, ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value": "value" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_search", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web_search", Result: `{ "value": "result1" }`, ProviderExecuted: true, ProviderExecutedSet: true, ProviderMetadata: r72streamPMD("result-1")}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "web_search", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-2", ToolName: "web_search", Result: "ERROR", IsError: &isErr, ProviderExecuted: true, ProviderExecutedSet: true, ProviderMetadata: r72streamPMD("error-1")}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
}

var r72streamWebSearch = types.Tool{Name: "web_search", Type: "provider", ProviderID: "test.web_search", ProviderExecuted: true, Parameters: r71streamValueSchema}

func TestR72StreamUIProviderExecutedToolCallAndResult(t *testing.T) {
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, r72streamProviderExecutedSteps()), Prompt: "test-input", Tools: []types.Tool{r72streamWebSearch}}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"providerExecuted":true,"toolCallId":"call-1","toolName":"web_search","type":"tool-input-start"},
		{"inputTextDelta":"{ \"value\": \"value\" }","toolCallId":"call-1","type":"tool-input-delta"},
		{"input":{"value":"value"},"providerExecuted":true,"toolCallId":"call-1","toolName":"web_search","type":"tool-input-available"},
		{"output":"{ \"value\": \"result1\" }","providerExecuted":true,"providerMetadata":{"provider":{"itemId":"result-1"}},"toolCallId":"call-1","type":"tool-output-available"},
		{"input":{"value":"value"},"providerExecuted":true,"toolCallId":"call-2","toolName":"web_search","type":"tool-input-available"},
		{"errorText":"ERROR","providerExecuted":true,"providerMetadata":{"provider":{"itemId":"error-1"}},"toolCallId":"call-2","type":"tool-output-error"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`)
}

// TS 19446: dynamic tools > single dynamic tool call and result > should
// include dynamic tool call and result in the ui message stream.
func TestR72StreamUIDynamicToolCallAndResult(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "dynamicTool"}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value": "value" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "dynamicTool", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
	}
	dyn := types.Tool{Name: "dynamicTool", Type: types.ToolTypeDynamic, Parameters: r71streamValueSchema,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"value": "test-result"}, nil
		}}
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Tools: []types.Tool{dyn}}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"dynamic":true,"toolCallId":"call-1","toolName":"dynamicTool","type":"tool-input-start"},
		{"inputTextDelta":"{ \"value\": \"value\" }","toolCallId":"call-1","type":"tool-input-delta"},
		{"dynamic":true,"input":{"value":"value"},"toolCallId":"call-1","toolName":"dynamicTool","type":"tool-input-available"},
		{"dynamic":true,"output":{"value":"test-result"},"toolCallId":"call-1","type":"tool-output-available"},
		{"type":"finish-step"},
		{"finishReason":"tool-calls","type":"finish"}]`)
}

// TS 18672 / 18645: the same stream's content and steps. A provider result
// flagged isError (no Go Error set) is a tool-error part whose error is the
// result value, and it is not one of the step's tool results.
func TestR72StreamProviderExecutedIsErrorResultIsToolError(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r72streamProviderExecutedSteps()), Prompt: "test-input", Tools: []types.Tool{r72streamWebSearch}, StopWhen: []StopCondition{StepCountIs(4)}})
	steps := r.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	content := steps[0].Content
	if len(content) != 4 {
		t.Fatalf("content = %#v, want 4 parts", content)
	}
	ok, isResult := content[1].(types.ToolResultContent)
	if !isResult || ok.ToolCallID != "call-1" || ok.Output == nil || ok.Output.Value != `{ "value": "result1" }` || !ok.ProviderExecuted {
		t.Fatalf("content[1] = %#v, want call-1 tool-result", content[1])
	}
	te, isErrPart := content[3].(types.ToolErrorContent)
	if !isErrPart {
		t.Fatalf("content[3] = %#v, want tool-error", content[3])
	}
	if te.ToolCallID != "call-2" || te.ToolName != "web_search" || te.Error != "ERROR" || !te.ProviderExecuted ||
		string(te.ProviderMetadata) != `{"provider":{"itemId":"error-1"}}` || te.Input["value"] != "value" {
		t.Fatalf("tool-error = %#v", te)
	}
	if got := steps[0].ToolResults; len(got) != 1 || got[0].ToolCallID != "call-1" {
		t.Fatalf("step tool results = %#v, want call-1 only", got)
	}
	if got := r.ToolResults(); len(got) != 1 || got[0].ToolCallID != "call-1" {
		t.Fatalf("result tool results = %#v, want call-1 only", got)
	}
}

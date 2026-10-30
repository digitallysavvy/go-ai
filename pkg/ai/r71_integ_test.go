package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r71integToolErrors returns the tool-error parts of content: since the
// owner's r71 ruling (TS toolResults = tool-result parts only), a failed tool
// call is reported there and not in ToolResults.
func r71integToolErrors(content []types.ContentPart) []types.ToolErrorContent {
	var out []types.ToolErrorContent
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolErrorContent:
			out = append(out, p)
		case *types.ToolErrorContent:
			if p != nil {
				out = append(out, *p)
			}
		}
	}
	return out
}

// r71integWebSearchModel returns the TS 'should not synthesize a client tool
// error for an invalid provider-executed tool call' response: a
// provider-executed web_search call with input {} and the provider's isError
// result.
func r71integWebSearchModel(calls *int) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		*calls++
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "call-1", ToolName: "web_search", ProviderExecuted: true, IsError: r71integTrue(),
					Result: map[string]interface{}{"type": "web_search_tool_result_error", "errorCode": "invalid_tool_input"}},
			},
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "web_search", Arguments: map[string]interface{}{}, RawArguments: "{}", ProviderExecuted: true}},
			FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_use",
		}, nil
	}}
}

func r71integTrue() *bool { b := true; return &b }

var r71integWebSearchTool = types.Tool{
	Name: "web_search", Type: types.ToolTypeProviderDefined, ProviderExecuted: true, ProviderID: "test.web_search",
	Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}},
}

// TS generate-text.test.ts 'should not synthesize a client tool error for an
// invalid provider-executed tool call' (gen CR-A + CR-B): the provider tool's
// input is validated, so the call is invalid and dynamic; the isError result
// is a tool-error part (dynamic from the call, input {}) and is replayed as
// error-json; no client tool error is added and ToolResults is empty.
func TestR71Integ_InvalidProviderExecutedCallAndIsErrorResult(t *testing.T) {
	calls := 0
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71integWebSearchModel(&calls), Tools: []types.Tool{r71integWebSearchTool}, Prompt: "Search the web.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content = %#v, want the call and the tool error only", result.Content)
	}
	call, ok := result.Content[0].(types.ToolCallContent)
	if !ok || !call.Invalid || !call.Dynamic || !call.ProviderExecuted || call.ToolCallID != "call-1" {
		t.Fatalf("content[0] = %#v, want an invalid dynamic provider-executed call", result.Content[0])
	}
	if msg, _ := call.Error.(string); !strings.Contains(msg, "Invalid input for tool web_search") {
		t.Errorf("call error = %v", call.Error)
	}
	toolErr, ok := result.Content[1].(types.ToolErrorContent)
	if !ok || !toolErr.ProviderExecuted || !toolErr.Dynamic || toolErr.ToolCallID != "call-1" || toolErr.ToolName != "web_search" ||
		len(toolErr.Input) != 0 || toolErr.Input == nil || toolErr.ErrorText != "" {
		t.Fatalf("content[1] = %#v, want a provider tool-error", result.Content[1])
	}
	if got, _ := json.Marshal(toolErr.Error); string(got) != `{"errorCode":"invalid_tool_input","type":"web_search_tool_result_error"}` {
		t.Errorf("tool error value = %s", got)
	}
	if len(result.ToolResults) != 0 || len(result.Steps[0].ToolResults) != 0 || len(result.DynamicToolResults) != 0 {
		t.Errorf("tool results = %+v, want none (TS: tool-error parts are not tool results)", result.ToolResults)
	}
	msgs := result.ResponseMessages
	if len(msgs) != 1 || msgs[0].Role != types.RoleAssistant || len(msgs[0].Content) != 2 {
		t.Fatalf("response messages = %#v, want one assistant message", msgs)
	}
	replay, ok := msgs[0].Content[1].(types.ToolResultContent)
	if !ok || replay.Output == nil || replay.Output.Type != types.ToolResultOutputErrorJSON {
		t.Fatalf("replayed result = %#v, want error-json", msgs[0].Content[1])
	}
	if got, _ := json.Marshal(replay.Output.Value); string(got) != `{"errorCode":"invalid_tool_input","type":"web_search_tool_result_error"}` {
		t.Errorf("error-json value = %s", got)
	}

	// The Anthropic converter turns the replayed error-json back into a
	// web_search_tool_result_error block (TS convert-to-anthropic-prompt).
	prompt, err := promptutils.ConvertToAnthropicPrompt(append([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Search the web."}}}}, msgs...), promptutils.AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(prompt.Messages)
	if !strings.Contains(string(wire), `"content":{"error_code":"invalid_tool_input","type":"web_search_tool_result_error"}`) {
		t.Errorf("anthropic prompt = %s, want a web_search_tool_result_error with the error code", wire)
	}
}

// The provider tool input validation also uses the tool's defaults and
// passes a valid call through unchanged (TS parseToolCall for provider
// tools).
func TestR71Integ_ValidProviderExecutedCallStaysValid(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "web_search", Input: `{"query":"go"}`, ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "call-1", ToolName: "web_search", ProviderExecuted: true, Result: []interface{}{"hit"}},
			},
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "web_search", Arguments: map[string]interface{}{"query": "go"}, RawArguments: `{"query":"go"}`, ProviderExecuted: true}},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Tools: []types.Tool{r71integWebSearchTool}, Prompt: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if call, ok := result.Content[0].(types.ToolCallContent); !ok || call.Invalid || call.Dynamic {
		t.Fatalf("content[0] = %#v, want a valid static call", result.Content[0])
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error != nil {
		t.Fatalf("tool results = %+v, want the provider result", result.ToolResults)
	}
}

type r71integStepTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	steps [][]types.ToolResult
	final []types.ToolResult
}

func (r *r71integStepTelemetry) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, e.ToolResults)
}

func (r *r71integStepTelemetry) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.final = e.ToolResults
}

func r71integFailingTools() []types.Tool {
	return []types.Tool{
		{Name: "ok", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "fine", nil
		}},
		{Name: "bad", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("boom")
		}},
	}
}

var r71integFailingCalls = []types.ToolCall{
	{ID: "c-ok", ToolName: "ok", Arguments: map[string]interface{}{}},
	{ID: "c-bad", ToolName: "bad", Arguments: map[string]interface{}{}},
}

// r71integCheckOnlySuccess checks a ToolResults list holds only the
// successful call (owner ruling r71: TS toolResults are the tool-result
// parts; a failed call is a tool-error part only).
func r71integCheckOnlySuccess(t *testing.T, name string, results []types.ToolResult) {
	t.Helper()
	if len(results) != 1 || results[0].ToolCallID != "c-ok" || results[0].Error != nil {
		t.Errorf("%s = %+v, want only c-ok", name, results)
	}
}

// Owner ruling r71 (gen CR-C): GenerateText's result, step, step and end
// events hold only successful tool results; the failure is a tool-error
// content part, still in the next prompt as error-text, and still in the
// telemetry step end (which has no content).
func TestR71Integ_GenerateTextToolResultsExcludeFailures(t *testing.T) {
	var prompts [][]types.Message
	calls := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		prompts = append(prompts, o.Prompt.Messages)
		calls++
		if calls == 1 {
			return &types.GenerateResult{ToolCalls: r71integFailingCalls, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
	tel := &r71integStepTelemetry{}
	var stepEvent OnStepFinishEvent
	var endEvent OnFinishEvent
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "p", Tools: r71integFailingTools(), StopWhen: []StopCondition{StepCountIs(2)},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}},
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) {
			if e.StepNumber == 0 {
				stepEvent = e
			}
		},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { endEvent = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	r71integCheckOnlySuccess(t, "result.ToolResults", result.ToolResults)
	r71integCheckOnlySuccess(t, "result.StaticToolResults", result.StaticToolResults)
	r71integCheckOnlySuccess(t, "steps[0].ToolResults", result.Steps[0].ToolResults)
	r71integCheckOnlySuccess(t, "steps[0].StaticToolResults", result.Steps[0].StaticToolResults)
	r71integCheckOnlySuccess(t, "OnStepEndEvent.ToolResults", stepEvent.ToolResults)
	r71integCheckOnlySuccess(t, "OnEndEvent.ToolResults", endEvent.ToolResults)
	if toolErrors := r71integToolErrors(result.Steps[0].Content); len(toolErrors) != 1 || toolErrors[0].ToolCallID != "c-bad" || toolErrors[0].ErrorText != "Error: boom" {
		t.Errorf("tool errors = %+v, want c-bad", toolErrors)
	}
	if !strings.Contains(r71integJSON(t, prompts[1]), `"output":{"type":"error-text","value":"Error: boom"}`) {
		t.Errorf("second prompt = %s, want the error-text result", r71integJSON(t, prompts[1]))
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.steps) != 2 || len(tel.steps[0]) != 2 || tel.steps[0][1].Error == nil {
		t.Errorf("telemetry step end tool results = %+v, want both calls (failure included)", tel.steps)
	}
	r71integCheckOnlySuccess(t, "telemetry end ToolResults", tel.final)
}

// The same for StreamText: ToolResults(), StaticToolResults(), the steps
// and the end event hold only the successful call.
func TestR71Integ_StreamTextToolResultsExcludeFailures(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &r71integFailingCalls[0]},
				{Type: provider.ChunkTypeToolCall, ToolCall: &r71integFailingCalls[1]},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	tel := &r71integStepTelemetry{}
	var endEvent OnFinishEvent
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "p", Tools: r71integFailingTools(), StopWhen: []StopCondition{StepCountIs(2)},
		Telemetry:  &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { endEvent = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	r71integCheckOnlySuccess(t, "ToolResults()", res.ToolResults())
	r71integCheckOnlySuccess(t, "StaticToolResults()", res.StaticToolResults())
	steps := res.Steps()
	r71integCheckOnlySuccess(t, "steps[0].ToolResults", steps[0].ToolResults)
	r71integCheckOnlySuccess(t, "OnEndEvent.ToolResults", endEvent.ToolResults)
	if toolErrors := r71integToolErrors(steps[0].Content); len(toolErrors) != 1 || toolErrors[0].ToolCallID != "c-bad" {
		t.Errorf("tool errors = %+v, want c-bad", toolErrors)
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.steps) != 2 || len(tel.steps[0]) != 2 {
		t.Errorf("telemetry step end tool results = %+v, want both calls (failure included)", tel.steps)
	}
}

// A provider isError result in a stream (TS stream-language-model-call
// 'tool-result' with isError: a tool-error part) is not one of the
// ToolResults either.
func TestR71Integ_StreamTextProviderErrorNotAToolResult(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "web_search", Arguments: map[string]interface{}{"query": "go"}, ProviderExecuted: true}
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &call},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web_search", ProviderExecuted: true,
				IsError: r71integTrue(), Error: errors.New("invalid_tool_input"), Result: map[string]interface{}{"type": "web_search_tool_result_error", "errorCode": "invalid_tool_input"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Tools: []types.Tool{r71integWebSearchTool}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.ToolResults(); len(got) != 0 {
		t.Errorf("ToolResults() = %+v, want none", got)
	}
	if toolErrors := r71integToolErrors(res.Steps()[0].Content); len(toolErrors) != 1 || !toolErrors[0].ProviderExecuted {
		t.Errorf("tool errors = %+v, want the provider error", toolErrors)
	}
}

func r71integJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// r71integProviderErrorParts is a provider stream with one successful and
// one failed provider-executed result.
func r71integProviderErrorParts(lead ...provider.StreamChunk) []provider.StreamChunk {
	okCall := types.ToolCall{ID: "p-ok", ToolName: "web_search", Arguments: map[string]interface{}{"query": "a"}, ProviderExecuted: true}
	badCall := types.ToolCall{ID: "p-bad", ToolName: "web_search", Arguments: map[string]interface{}{"query": "b"}, ProviderExecuted: true}
	return append(lead,
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &okCall},
		provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "p-ok", ToolName: "web_search", ProviderExecuted: true, Result: "hit"}},
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &badCall},
		provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "p-bad", ToolName: "web_search", ProviderExecuted: true, IsError: r71integTrue(), Error: errors.New("unavailable")}},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	)
}

func r71integCheckProviderOK(t *testing.T, name string, results []types.ToolResult) {
	t.Helper()
	if len(results) != 1 || results[0].ToolCallID != "p-ok" {
		t.Errorf("%s = %+v, want only p-ok", name, results)
	}
}

// A hand-built result's single-step read (readAllLegacy) and an external
// (harness) result hold only the successful provider result as well.
func TestR71Integ_LegacyReadAndExternalResultExcludeFailures(t *testing.T) {
	legacy := &StreamTextResult{stream: testutil.NewMockTextStream(r71integProviderErrorParts())}
	if _, err := legacy.ReadAll(); err != nil {
		t.Fatal(err)
	}
	r71integCheckProviderOK(t, "legacy ToolResults()", legacy.ToolResults())

	parts := r71integProviderErrorParts(provider.StreamChunk{Type: provider.ChunkTypeStartStep})
	last := parts[len(parts)-1]
	parts = append(parts[:len(parts)-1], provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop}, last)
	external := NewStreamTextResultFromParts(context.Background(), testutil.NewMockTextStream(parts), ExternalStreamOptions{})
	if _, err := external.ReadAll(); err != nil {
		t.Fatal(err)
	}
	r71integCheckProviderOK(t, "external ToolResults()", external.ToolResults())
	steps := external.Steps()
	if len(steps) != 1 {
		t.Fatalf("external steps = %d", len(steps))
	}
	r71integCheckProviderOK(t, "external steps[0].ToolResults", steps[0].ToolResults)
	r71integCheckProviderOK(t, "external steps[0].StaticToolResults", steps[0].StaticToolResults)
}

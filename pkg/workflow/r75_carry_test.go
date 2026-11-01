package workflow

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The resume start-gate chain is transitive (r74 ruling, as pkg/ai and the
// legacy loop): call-2 returns early (input revalidation fails), so it must
// wait for call-1's start before releasing call-3. Otherwise call-3 fires
// its start while call-1 is still revalidating, and TS mapWithConcurrency
// order (call-1, call-3) is lost.
func TestR75Carry_ApprovedToolStartGateTransitiveOverEarlyReturn(t *testing.T) {
	call3Started := make(chan struct{})
	var call3Once sync.Once
	var mu sync.Mutex
	var starts []string
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "tool1",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"n": map[string]interface{}{"type": "number"}},
				"required":   []interface{}{"n"},
			},
			// call-1's approval policy is slow: it waits until call-3 has
			// fired its start (the bug) or a short timeout (the fix: call-3
			// cannot start before call-1).
			ToolApproval: types.ToolNeedsApprovalFunc(func(_ context.Context, _ map[string]interface{}, o types.ToolNeedsApprovalOptions) bool {
				if o.ToolCallID == "call-1" {
					select {
					case <-call3Started:
					case <-time.After(300 * time.Millisecond):
					}
				}
				return true
			}),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			mu.Lock()
			starts = append(starts, e.ToolCall.ID)
			mu.Unlock()
			if e.ToolCall.ID == "call-3" {
				call3Once.Do(func() { close(call3Started) })
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := r74carryApprovedMessages("call-1", "call-2", "call-3")
	msgs[1].Content[0] = types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"n": 1}}
	// call-2 has invalid input: revalidation fails before any start callback.
	msgs[1].Content[4] = types.ToolCallContent{ToolCallID: "call-3", ToolName: "tool1", Arguments: map[string]interface{}{"n": 3}}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Steps()
	mu.Lock()
	got := strings.Join(starts, ",")
	mu.Unlock()
	if got != "call-1,call-3" {
		t.Errorf("start order = %s, want call-1,call-3", got)
	}
	if tr := lastToolResultInPrompt(t, model, "call-2"); tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText {
		t.Errorf("call-2 result = %+v, want error-text", tr.Output)
	}
}

func r75carryScripted(steps ...[]provider.StreamChunk) (*testutil.MockLanguageModel, *[]*provider.GenerateOptions) {
	var calls []*provider.GenerateOptions
	m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "mock-model", ToolSupport: true}
	m.DoStreamFunc = func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
		i := len(calls)
		calls = append(calls, opts)
		if i >= len(steps) {
			return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
		}
		return testutil.NewMockTextStream(steps[i]), nil
	}
	return m, &calls
}

// r75carryAssertContent compares a message's content parts, as JSON, with
// want (a TS-shaped JSON array).
func r75carryAssertContent(t *testing.T, label string, msg types.Message, want string) {
	t.Helper()
	got, err := json.Marshal(msg.Content)
	if err != nil {
		t.Fatal(err)
	}
	var g, w interface{}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s content =\n%s\nwant\n%s", label, got, want)
	}
}

func r75carryRunStream(t *testing.T, agent *WorkflowAgent) []types.StepResult {
	t.Helper()
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return stream.Steps()
}

// TS model-call-iterator.test.ts "conversation prompt" / "providerMetadata
// to providerOptions mapping": the continuation prompt keeps reasoning, text
// and tool calls in emission order with their provider metadata as
// providerOptions, interleaves the provider-executed result after its call
// in the assistant message, and puts the client result in a tool message.
func TestR75Carry_ContinuationPromptOrderAndProviderOptions(t *testing.T) {
	model, calls := r75carryScripted([]provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "r1", ProviderMetadata: json.RawMessage(`{"google":{"thoughtSignature":"sig-r"}}`)},
		{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "think"},
		{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
		{Type: provider.ChunkTypeTextStart, ID: "t1", ProviderMetadata: json.RawMessage(`{"google":{"thoughtSignature":"sig-text"}}`)},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "I found the answer before calling the tool."},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "provider-call", ToolName: "providerTool", Arguments: map[string]interface{}{"query": "docs"}, ProviderExecuted: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "provider-call", ToolName: "providerTool", Result: []interface{}{map[string]interface{}{"toolName": "clientTool"}}, ProviderExecuted: true}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "client-call", ToolName: "clientTool", Arguments: map[string]interface{}{"id": 1}, ProviderMetadata: map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig_abc123_test_signature"}}}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "plain-call", ToolName: "clientTool", Arguments: map[string]interface{}{"id": 2}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{
			{Name: "clientTool", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"result": "success"}, nil
			}},
			{Name: "providerTool", Type: "provider", ProviderID: "mock.provider_tool", ProviderExecuted: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r75carryRunStream(t, agent)
	if len(*calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(*calls))
	}
	msgs := (*calls)[1].Prompt.Messages
	if len(msgs) != 3 || msgs[1].Role != types.RoleAssistant || msgs[2].Role != types.RoleTool {
		t.Fatalf("continuation prompt roles = %+v", msgs)
	}
	r75carryAssertContent(t, "assistant", msgs[1], `[
		{"type":"reasoning","text":"think","providerOptions":{"google":{"thoughtSignature":"sig-r"}}},
		{"type":"text","text":"I found the answer before calling the tool.","providerOptions":{"google":{"thoughtSignature":"sig-text"}}},
		{"type":"tool-call","toolCallId":"provider-call","toolName":"providerTool","input":{"query":"docs"},"providerExecuted":true},
		{"type":"tool-result","toolCallId":"provider-call","toolName":"providerTool","output":{"type":"json","value":[{"toolName":"clientTool"}]}},
		{"type":"tool-call","toolCallId":"client-call","toolName":"clientTool","input":{"id":1},"providerOptions":{"google":{"thoughtSignature":"sig_abc123_test_signature"}}},
		{"type":"tool-call","toolCallId":"plain-call","toolName":"clientTool","input":{"id":2}}
	]`)
	var ids []string
	for _, p := range msgs[2].Content {
		if tr, ok := p.(types.ToolResultContent); ok {
			ids = append(ids, tr.ToolCallID)
		}
	}
	if strings.Join(ids, ",") != "client-call,plain-call" {
		t.Errorf("tool message results = %v, want client-call,plain-call", ids)
	}
}

// TS workflow-agent.test.ts "should use toModelOutput for provider-executed
// tool results while preserving raw output": the local execute is not
// called, toModelOutput gets {toolCallId, input, output}, the history result
// is its output with the provider result's metadata as providerOptions, and
// the step's tool result keeps the raw output.
func TestR75Carry_ProviderExecutedResultUsesToModelOutput(t *testing.T) {
	raw := map[string]interface{}{"public": "provider result", "secret": "hide me"}
	model, _ := r75carryScripted([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "provider-call-id", ToolName: "WebSearch", Arguments: map[string]interface{}{"query": "test query"}, ProviderExecuted: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "provider-call-id", ToolName: "WebSearch", Result: raw, ProviderExecuted: true, ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "provider-result-item"}}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
	executed := false
	var tmo []types.ToModelOutputOptions
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "WebSearch",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executed = true
				return nil, nil
			},
			ToModelOutput: func(_ context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				tmo = append(tmo, o)
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees: " + o.Output.(map[string]interface{})["public"].(string)}, nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := r75carryRunStream(t, agent)
	if executed {
		t.Error("local execute ran for a provider-executed call")
	}
	if len(tmo) != 1 || tmo[0].ToolCallID != "provider-call-id" || !reflect.DeepEqual(tmo[0].Input, map[string]interface{}{"query": "test query"}) || !reflect.DeepEqual(tmo[0].Output, raw) {
		t.Fatalf("toModelOutput calls = %+v", tmo)
	}
	if len(steps) != 1 || len(steps[0].Response.Messages) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	r75carryAssertContent(t, "response", steps[0].Response.Messages[0], `[
		{"type":"tool-call","toolCallId":"provider-call-id","toolName":"WebSearch","input":{"query":"test query"},"providerExecuted":true},
		{"type":"tool-result","toolCallId":"provider-call-id","toolName":"WebSearch","output":{"type":"text","value":"model sees: provider result"},"providerOptions":{"openai":{"itemId":"provider-result-item"}}}
	]`)
	if len(steps[0].ToolResults) != 1 || !reflect.DeepEqual(steps[0].ToolResults[0].Result, raw) {
		t.Errorf("step tool results = %+v, want raw output", steps[0].ToolResults)
	}
}

// TS model-call-iterator.test.ts "preserves file and text order in a
// subsequent tool-call turn" and "omits empty text from file-bearing
// tool-call message history"; sources stay out of the history ("preserves
// sources in step results without adding them to message history").
func TestR75Carry_ContinuationPromptFilesTextSources(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"Use this file.", `[
			{"type":"file","fileData":{"type":"data","data":"ZmlsZS1jb250ZW50"},"data":"ZmlsZS1jb250ZW50","mimeType":"text/plain","mediaType":"text/plain"},
			{"type":"text","text":"Use this file."},
			{"type":"tool-call","toolCallId":"call-1","toolName":"testTool","input":{"query":"test"}}
		]`},
		{"", `[
			{"type":"file","fileData":{"type":"data","data":"ZmlsZS1jb250ZW50"},"data":"ZmlsZS1jb250ZW50","mimeType":"text/plain","mediaType":"text/plain"},
			{"type":"tool-call","toolCallId":"call-1","toolName":"testTool","input":{"query":"test"}}
		]`},
	} {
		model, calls := r75carryScripted([]provider.StreamChunk{
			{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{Data: []byte("file-content"), MediaType: "text/plain"}},
			{Type: provider.ChunkTypeTextStart, ID: "t1"},
			{Type: provider.ChunkTypeText, ID: "t1", Text: tc.text},
			{Type: provider.ChunkTypeTextEnd, ID: "t1"},
			{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "source-1", URL: "https://example.com/source", Title: "Example source"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"query": "test"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		})
		agent, err := NewWorkflowAgent(WorkflowAgent{
			Model: model,
			Tools: []types.Tool{{Name: "testTool", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"result": "success"}, nil
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		steps := r75carryRunStream(t, agent)
		if len(*calls) != 2 {
			t.Fatalf("text=%q: model calls = %d, want 2", tc.text, len(*calls))
		}
		r75carryAssertContent(t, "assistant text="+tc.text, (*calls)[1].Prompt.Messages[1], tc.want)
		if len(steps) == 0 || len(steps[0].Sources) != 1 {
			t.Errorf("text=%q: step sources = %+v, want the source", tc.text, steps)
		}
	}
}

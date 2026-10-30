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
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r71genApprovalHistory is the TS approval-resume history: the user prompt,
// the assistant tool-call and its approval request id-1, and the tool
// message with the approval response (plus extra tool parts).
func r71genApprovalHistory(approved bool, reason string, extra ...types.ContentPart) []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test-input"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}},
			types.ToolApprovalRequestContent{ApprovalID: "id-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: append([]types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "id-1", Approved: approved, Reason: reason}}, extra...)},
	}
}

func r71genTool1(execute func() (interface{}, error)) []types.Tool {
	return []types.Tool{{Name: "tool1",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return execute()
		}}}
}

// r71genHelloModel answers every call with "Hello, world!" and records the
// prompts.
func r71genHelloModel(prompts *[][]types.Message) *testutil.MockLanguageModel {
	var mu sync.Mutex
	return &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		*prompts = append(*prompts, o.Prompt.Messages)
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "Hello, world!"}}, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"}, nil
	}}
}

var (
	r71genUserPrompt     = r71genObj{"role": "user", "content": []interface{}{r71genObj{"type": "text", "text": "test-input"}}}
	r71genAssistantCall  = r71genObj{"role": "assistant", "content": []interface{}{r71genObj{"type": "tool-call", "toolCallId": "call-1", "toolName": "tool1", "input": r71genObj{"value": "value"}}}}
	r71genHelloAssistant = r71genObj{"role": "assistant", "content": []interface{}{r71genObj{"type": "text", "text": "Hello, world!"}}}
)

func r71genToolMessage(output r71genObj) r71genObj {
	return r71genObj{"role": "tool", "content": []interface{}{r71genObj{"type": "tool-result", "toolCallId": "call-1", "toolName": "tool1", "output": output}}}
}

// generate-text.test.ts 'tool execution approval' > 'when a call from a single
// tool that needs approval is approved': the tool runs once, the model's
// prompt holds the tool result without the approval parts, the response
// messages start with the tool result, and prepareStep sees it in
// responseMessages ('should pass the initial tool result in responseMessages
// to prepareStep').
func TestR71Gen_ApprovedToolCallResume(t *testing.T) {
	var prompts [][]types.Message
	var executions int
	var prepareResponse []interface{}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71genHelloModel(&prompts),
		Tools: r71genTool1(func() (interface{}, error) {
			executions++
			return "result1", nil
		}),
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Messages:     r71genApprovalHistory(true, ""),
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			prepareResponse = append(prepareResponse, r71genJSON(t, s.ResponseMessages))
			return s
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executions != 1 {
		t.Errorf("tool executions = %d, want 1", executions)
	}
	toolResult := r71genToolMessage(r71genObj{"type": "text", "value": "result1"})
	r71genCheckJSON(t, "prompts", r71genJSON(t, prompts), []interface{}{[]interface{}{r71genUserPrompt, r71genAssistantCall, toolResult}})
	r71genCheckJSON(t, "result.responseMessages", r71genJSON(t, res.ResponseMessages), []interface{}{toolResult, r71genHelloAssistant})
	r71genCheckJSON(t, "prepareStep responseMessages", prepareResponse, []interface{}{[]interface{}{toolResult}})
}

// 'when a call from a single tool that needs approval is approved and the tool
// throws' > 'should serialize the tool error as error-text in the
// continuation prompt'.
func TestR71Gen_ApprovedToolThrowsErrorTextInPrompt(t *testing.T) {
	var prompts [][]types.Message
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71genHelloModel(&prompts),
		Tools: r71genTool1(func() (interface{}, error) {
			return nil, errors.New("No valid token for plugin")
		}),
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Messages:     r71genApprovalHistory(true, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	r71genCheckJSON(t, "prompts", r71genJSON(t, prompts), []interface{}{[]interface{}{
		r71genUserPrompt, r71genAssistantCall,
		r71genToolMessage(r71genObj{"type": "error-text", "value": "Error: No valid token for plugin"}),
	}})
}

// 'should not duplicate an existing execution-denied result': a denied
// approval whose execution-denied result is already in the history reaches
// the model once and adds nothing to the response messages.
func TestR71Gen_DeniedApprovalNotDuplicated(t *testing.T) {
	var prompts [][]types.Message
	executions := 0
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71genHelloModel(&prompts),
		Tools: r71genTool1(func() (interface{}, error) {
			executions++
			return nil, nil
		}),
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Messages: r71genApprovalHistory(false, "User denied the request", types.ToolResultContent{
			ToolCallID: "call-1", ToolName: "tool1",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User denied the request"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	count := func(v interface{}) int {
		return strings.Count(string(r71genMarshal(t, v)), "execution-denied")
	}
	if got, gotResp := count(prompts), count(res.ResponseMessages); got != 1 || gotResp != 0 || executions != 0 {
		t.Errorf("promptExecutionDeniedOccurrences=%d responseExecutionDeniedOccurrences=%d executions=%d, want 1/0/0", got, gotResp, executions)
	}
	r71genCheckJSON(t, "prompts", r71genJSON(t, prompts), []interface{}{[]interface{}{
		r71genUserPrompt, r71genAssistantCall,
		r71genToolMessage(r71genObj{"type": "execution-denied", "reason": "User denied the request"}),
	}})
}

func r71genMCPHistory(approved bool, reason string) []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Shorten this URL: https://example.com"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, ProviderExecuted: true},
			types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "mcp-approval-1", Approved: approved, Reason: reason, ProviderExecuted: true}}},
	}
}

var r71genMCPTools = []types.Tool{{Name: "mcp_tool", ProviderExecuted: true,
	Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}}}}

var (
	r71genMCPUser = r71genObj{"role": "user", "content": []interface{}{r71genObj{"type": "text", "text": "Shorten this URL: https://example.com"}}}
	r71genMCPCall = r71genObj{"role": "assistant", "content": []interface{}{r71genObj{"type": "tool-call", "toolCallId": "mcp-call-1", "toolName": "mcp_tool", "input": r71genObj{"query": "test"}, "providerExecuted": true}}}
)

// 'provider-executed tool (MCP) approval' > 'when a provider-executed tool
// approval is approved': the approval response reaches the model exactly
// once (no local execution, no synthetic result), and the provider's call,
// result and text make up the content.
func TestR71Gen_ProviderExecutedApprovalApprovedResume(t *testing.T) {
	var prompts [][]types.Message
	calls := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		prompts = append(prompts, o.Prompt.Messages)
		calls++
		if calls == 1 {
			return &types.GenerateResult{
				Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Input: `{ "query": "test" }`, ProviderExecuted: true},
					types.ToolResultContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Result: map[string]interface{}{"shortened_url": "https://short.url/abc"}, ProviderExecuted: true},
					types.TextContent{Text: "Here is your shortened URL: https://short.url/abc"},
				},
				ToolCalls:    []types.ToolCall{{ID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, RawArguments: `{ "query": "test" }`, ProviderExecuted: true}},
				FinishReason: types.FinishReasonStop, RawFinishReason: "stop",
			}, nil
		}
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "Done"}}, FinishReason: types.FinishReasonStop}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Tools: r71genMCPTools, StopWhen: []StopCondition{IsStepCount(3)}, Messages: r71genMCPHistory(true, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
	r71genCheckJSON(t, "prompts[0]", r71genPromptJSON(t, prompts[0]), []interface{}{
		r71genMCPUser, r71genMCPCall,
		r71genObj{"role": "tool", "content": []interface{}{r71genObj{"type": "tool-approval-response", "approvalId": "mcp-approval-1", "approved": true}}},
	})
	if len(res.Content) != 3 {
		t.Fatalf("content = %#v", res.Content)
	}
	if c, ok := res.Content[0].(types.ToolCallContent); !ok || c.ToolCallID != "mcp-call-1" || c.ToolName != "mcp_tool" || !c.ProviderExecuted || c.Arguments["query"] != "test" {
		t.Errorf("content[0] = %#v", res.Content[0])
	}
	if r, ok := res.Content[1].(types.ToolResultContent); !ok || r.ToolCallID != "mcp-call-1" || !r.ProviderExecuted || r.Input["query"] != "test" || r.Dynamic ||
		r71genObjString(r.Result) != `{"shortened_url":"https://short.url/abc"}` {
		t.Errorf("content[1] = %#v", res.Content[1])
	}
	if x, ok := res.Content[2].(types.TextContent); !ok || x.Text != "Here is your shortened URL: https://short.url/abc" {
		t.Errorf("content[2] = %#v", res.Content[2])
	}
}

// 'when a provider-executed tool approval is denied': the denied response and
// an execution-denied result carrying the OpenAI approval id reach the model
// once; the content is the model's text and the run stops.
func TestR71Gen_ProviderExecutedApprovalDeniedResume(t *testing.T) {
	var prompts [][]types.Message
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		prompts = append(prompts, o.Prompt.Messages)
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "I understand. The tool execution was not approved."}}, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Tools: r71genMCPTools, StopWhen: []StopCondition{IsStepCount(3)}, Messages: r71genMCPHistory(false, "User denied the request"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Fatalf("model calls = %d, want 1", len(prompts))
	}
	r71genCheckJSON(t, "prompts[0]", r71genPromptJSON(t, prompts[0]), []interface{}{
		r71genMCPUser, r71genMCPCall,
		r71genObj{"role": "tool", "content": []interface{}{
			r71genObj{"type": "tool-approval-response", "approvalId": "mcp-approval-1", "approved": false, "reason": "User denied the request"},
			r71genObj{"type": "tool-result", "toolCallId": "mcp-call-1", "toolName": "mcp_tool", "output": r71genObj{
				"type": "execution-denied", "reason": "User denied the request",
				"providerOptions": r71genObj{"openai": r71genObj{"approvalId": "mcp-approval-1"}},
			}},
		}},
	})
	if len(res.Content) != 1 || res.FinishReason != types.FinishReasonStop {
		t.Fatalf("content = %#v, finishReason = %q", res.Content, res.FinishReason)
	}
	if x, ok := res.Content[0].(types.TextContent); !ok || x.Text != "I understand. The tool execution was not approved." {
		t.Errorf("content[0] = %#v", res.Content[0])
	}
}

func r71genObjString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// r71genPromptJSON is r71genJSON for a language model prompt. Go keeps the
// providerExecuted flag on a kept tool-approval-response part for its
// provider converters; the LanguageModelV4 part has no such field and no
// provider sends it (the gateway wire drops it), so it is left out here.
func r71genPromptJSON(t *testing.T, msgs []types.Message) interface{} {
	t.Helper()
	out := r71genJSON(t, msgs)
	for _, m := range out.([]interface{}) {
		for _, p := range m.(map[string]interface{})["content"].([]interface{}) {
			if part := p.(map[string]interface{}); part["type"] == "tool-approval-response" {
				delete(part, "providerExecuted")
			}
		}
	}
	return out
}

package xai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

func xaiOpt(k string, v interface{}) map[string]interface{} {
	return map[string]interface{}{"xai": map[string]interface{}{k: v}}
}

// TS convertToXaiResponsesInput: xAI item ids on assistant text and function
// calls, reasoning replay, verbatim tool errors, "tool execution denied".
func TestXAIResponsesInputMatchesTSConverter(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4")
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "sys"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "thinking", ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"itemId": "rs_1", "reasoningEncryptedContent": "enc"}}},
			types.ReasoningContent{Text: "nometa"},
			types.TextContent{Text: "ans", ProviderOptions: xaiOpt("itemId", "msg_1")},
			types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: `{"a":1}`, ProviderOptions: xaiOpt("itemId", "fc_1")},
			types.ToolCallContent{ToolCallID: "s1", ToolName: "web_search", Input: `{}`, ProviderExecuted: true},
			types.FileContent{Data: []byte("x"), MediaType: "image/png"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "c1", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "bad"}},
			types.ToolResultContent{ToolCallID: "c2", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}},
			types.ToolResultContent{ToolCallID: "c3", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: map[string]interface{}{"a": 1}}},
			types.ToolResultContent{ToolCallID: "c4", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
				types.TextContentBlock{Text: "t"},
				types.ImageContentBlock{Data: []byte("img"), MediaType: "image/png"},
			}}},
		}},
	}
	body, warnings, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}}, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body["input"])
	var got interface{}
	_ = json.Unmarshal(raw, &got)
	want := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
		map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "hi"}}},
		map[string]interface{}{"type": "reasoning", "id": "rs_1", "summary": []interface{}{map[string]interface{}{"type": "summary_text", "text": "thinking"}}, "status": "completed", "encrypted_content": "enc"},
		map[string]interface{}{"role": "assistant", "content": "ans", "id": "msg_1"},
		map[string]interface{}{"type": "function_call", "id": "fc_1", "call_id": "c1", "name": "f", "arguments": `{"a":1}`, "status": "completed"},
		map[string]interface{}{"type": "function_call_output", "call_id": "c1", "output": "bad"},
		map[string]interface{}{"type": "function_call_output", "call_id": "c2", "output": "tool execution denied"},
		map[string]interface{}{"type": "function_call_output", "call_id": "c3", "output": `{"a":1}`},
		map[string]interface{}{"type": "function_call_output", "call_id": "c4", "output": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "t"},
			map[string]interface{}{"type": "input_image", "image_url": "data:image/png;base64,aW1n"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Fatalf("input mismatch\n got  %s\n want %s", gj, wj)
	}
	var msgsW []string
	for _, w := range warnings {
		msgsW = append(msgsW, w.Message)
	}
	wantW := []string{
		"Reasoning parts without itemId or encrypted content cannot be sent back to xAI. Skipping.",
		"xAI Responses API does not support file in assistant messages",
	}
	if !reflect.DeepEqual(msgsW, wantW) {
		t.Fatalf("warnings = %q, want %q", msgsW, wantW)
	}
}

// TS doGenerate: reasoning parts need text or encrypted content, and a
// response without usage reports explicit zero tokens.
func TestXAIResponsesConvertResponseReasoningAndZeroUsage(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4")
	var resp responses.ResponsesAPIResponse
	if err := json.Unmarshal([]byte(`{"id":"r","status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[]},{"type":"reasoning","id":"rs_2","summary":[],"encrypted_content":"enc"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	res, err := m.convertResponse(resp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content = %#v, want only the encrypted reasoning part", res.Content)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 0 || res.Usage.InputDetails == nil || res.Usage.InputDetails.CacheWriteTokens == nil {
		t.Fatalf("usage = %+v, want explicit zeros", res.Usage)
	}
}

// TS resolves the registered name of an xai.image_generation tool for the
// provider-executed call and result.
func TestXAIResponsesImageGenerationUsesRegisteredToolName(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4")
	var resp responses.ResponsesAPIResponse
	if err := json.Unmarshal([]byte(`{"id":"r","status":"completed","output":[{"type":"image_generation_call","id":"ig_1","status":"completed","result":"b64"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	tools := []types.Tool{{Name: "painter", ProviderID: "xai.image_generation", Type: types.ToolTypeProviderDefined}}
	res, err := m.convertResponse(resp, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "painter" {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
}

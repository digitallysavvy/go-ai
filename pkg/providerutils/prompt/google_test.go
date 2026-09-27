package prompt

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func googleJSON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func googleOpts(opts map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"google": opts}
}

// TS convert-to-google-messages.test.ts: "does NOT inject the sentinel when server
// tool parts separate parallel function calls" + "injects the sentinel when a signed
// server tool call precedes an unsigned function call" (5e5453c).
func TestConvertToGoogleMessages_SentinelSkippedForSignedSibling(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "signed_function_call", ToolName: "weather", Arguments: map[string]interface{}{"location": "SF"},
			ProviderOptions: googleOpts(map[string]interface{}{"thoughtSignature": "function_signature"})},
		types.ToolCallContent{ToolCallID: "server_call", ToolName: "server:GOOGLE_SEARCH_WEB", Arguments: map[string]interface{}{"query": "weather"},
			ProviderOptions: googleOpts(map[string]interface{}{"serverToolCallId": "server_call", "serverToolType": "GOOGLE_SEARCH_WEB", "thoughtSignature": "server_call_signature"})},
		types.ToolResultContent{ToolCallID: "server_call", ToolName: "server:GOOGLE_SEARCH_WEB",
			Output:          &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"results": []interface{}{}}},
			ProviderOptions: googleOpts(map[string]interface{}{"serverToolCallId": "server_call", "serverToolType": "GOOGLE_SEARCH_WEB", "thoughtSignature": "server_response_signature"})},
		types.ToolCallContent{ToolCallID: "unsigned_function_call", ToolName: "weather", Arguments: map[string]interface{}{"location": "NYC"}},
	}}}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IsGemini3Model: true, IncludeFunctionCallIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	parts := out.Contents[0]["parts"].([]map[string]interface{})
	if len(parts) != 4 {
		t.Fatalf("parts = %s", googleJSON(t, parts))
	}
	if got := googleJSON(t, parts[2]); got != `{"thoughtSignature":"server_response_signature","toolResponse":{"id":"server_call","response":{"results":[]},"toolType":"GOOGLE_SEARCH_WEB"}}` {
		t.Errorf("toolResponse part = %s", got)
	}
	if got := googleJSON(t, parts[3]); got != `{"functionCall":{"args":{"location":"NYC"},"id":"unsigned_function_call","name":"weather"}}` {
		t.Errorf("unsigned call = %s", got)
	}
	if len(out.Warnings) != 0 {
		t.Errorf("warnings = %#v", out.Warnings)
	}

	msgs2 := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "server_call", ToolName: "server:GOOGLE_SEARCH_WEB", Arguments: map[string]interface{}{"query": "weather"},
			ProviderOptions: googleOpts(map[string]interface{}{"serverToolCallId": "server_call", "serverToolType": "GOOGLE_SEARCH_WEB", "thoughtSignature": "server_signature"})},
		types.ToolCallContent{ToolCallID: "function_call", ToolName: "weather", Arguments: map[string]interface{}{"location": "NYC"}},
	}}}
	out2, _ := ConvertToGoogleMessages(msgs2, GoogleMessagesOptions{IsGemini3Model: true, IncludeFunctionCallIDs: true})
	want := `[{"thoughtSignature":"server_signature","toolCall":{"args":{"query":"weather"},"id":"server_call","toolType":"GOOGLE_SEARCH_WEB"}},{"functionCall":{"args":{"location":"NYC"},"id":"function_call","name":"weather"},"thoughtSignature":"skip_thought_signature_validator"}]`
	if got := googleJSON(t, out2.Contents[0]["parts"]); got != want {
		t.Errorf("parts = %s", got)
	}
	if len(out2.Warnings) != 1 || !strings.Contains(out2.Warnings[0].Message, "`weather`") {
		t.Errorf("warnings = %#v", out2.Warnings)
	}
}

// TS: includeFunctionCallIds=false omits functionCall/functionResponse ids on Vertex (c57a353).
func TestConvertToGoogleMessages_OmitsFunctionCallIDs(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "sunny"}}}},
	}
	for _, include := range []bool{true, false} {
		out, _ := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IncludeFunctionCallIDs: include})
		fc := out.Contents[0]["parts"].([]map[string]interface{})[0]["functionCall"].(map[string]interface{})
		fr := out.Contents[1]["parts"].([]map[string]interface{})[0]["functionResponse"].(map[string]interface{})
		_, fcHasID := fc["id"]
		_, frHasID := fr["id"]
		if fcHasID != include || frHasID != include {
			t.Errorf("include=%v: functionCall id=%v functionResponse id=%v", include, fcHasID, frHasID)
		}
	}
}

// TestConvertToGoogleMessages_SerializesJSONSchemaReferenceInFunctionResponse
// ports TS convert-to-google-messages.test.ts "should serialize JSON Schema
// references in function response content" (ai@7.0.118 commit 8beac3e3ad):
// Google reserves {$ref: displayName} in structured function responses for
// multimodal parts, which conflicts with JSON Schema $ref, so a json tool
// result value containing "$ref" anywhere is JSON-stringified instead of
// forwarded as a nested object.
func TestConvertToGoogleMessages_SerializesJSONSchemaReferenceInFunctionResponse(t *testing.T) {
	toolResult := map[string]interface{}{
		"tools": []interface{}{
			map[string]interface{}{
				"name": "find_records",
				"inputSchema": map[string]interface{}{
					"$defs": map[string]interface{}{
						"Node": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"child": map[string]interface{}{"$ref": "#/$defs/Node"},
							},
						},
					},
					"$ref": "#/$defs/Node",
				},
			},
		},
	}
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID: "testCallId", ToolName: "get_schema",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: toolResult},
	}}}}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IncludeFunctionCallIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	fr := out.Contents[0]["parts"].([]map[string]interface{})[0]["functionResponse"].(map[string]interface{})
	response, ok := fr["response"].(map[string]interface{})
	if !ok {
		t.Fatalf("response = %#v, want a map", fr["response"])
	}
	content, ok := response["content"].(string)
	if !ok {
		t.Fatalf("content = %#v, want a JSON string", response["content"])
	}
	if want := googleJSON(t, toolResult); content != want {
		t.Errorf("content = %s, want %s", content, want)
	}
}

// TestConvertToGoogleMessages_CombinesConsecutiveToolMessages ports the
// core combining step (MergeConsecutiveToolMessages, hash 33647d7) applied
// ahead of Google conversion: two consecutive RoleTool SDK messages must
// become a SINGLE Google "user" turn with both functionResponse parts,
// instead of two separate "user" turns (previously: converter.go emitted one
// user turn per tool message).
func TestConvertToGoogleMessages_CombinesConsecutiveToolMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{
			{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}},
			{ID: "call-2", ToolName: "time", Arguments: map[string]interface{}{"tz": "PST"}},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "sunny"}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "call-2", ToolName: "time",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "3pm"}}}},
	}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IncludeFunctionCallIDs: true})
	if err != nil {
		t.Fatalf("ConvertToGoogleMessages error: %v", err)
	}
	// assistant turn + ONE combined user turn (not two).
	if len(out.Contents) != 2 {
		t.Fatalf("len(Contents) = %d, want 2 (assistant + one combined tool turn); got %#v", len(out.Contents), out.Contents)
	}
	userTurn := out.Contents[1]
	if userTurn["role"] != "user" {
		t.Fatalf("Contents[1].role = %v, want user", userTurn["role"])
	}
	parts, ok := userTurn["parts"].([]map[string]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("Contents[1].parts = %#v, want 2 functionResponse parts", userTurn["parts"])
	}
	fr1 := parts[0]["functionResponse"].(map[string]interface{})
	fr2 := parts[1]["functionResponse"].(map[string]interface{})
	if fr1["id"] != "call-1" || fr2["id"] != "call-2" {
		t.Errorf("functionResponse ids = %v, %v, want call-1, call-2", fr1["id"], fr2["id"])
	}
}

// TS: provider-executed code_execution replays as executableCode / codeExecutionResult (2db5621).
func TestConvertToGoogleMessages_CodeExecutionReplay(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "c1", ToolName: "code_execution", ProviderExecuted: true, Input: `{"language":"PYTHON","code":"print(1)"}`},
		types.ToolResultContent{ToolCallID: "c1", ToolName: "code_execution", ProviderExecuted: true,
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"outcome": "OUTCOME_OK", "output": "1\n"}}},
	}}}
	out, _ := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IsGemini3Model: true, IncludeFunctionCallIDs: true})
	want := `[{"executableCode":{"code":"print(1)","language":"PYTHON"}},{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1\n"}}]`
	if got := googleJSON(t, out.Contents[0]["parts"]); got != want {
		t.Errorf("parts = %s", got)
	}
	if len(out.Warnings) != 0 {
		t.Errorf("no sentinel warning expected for code execution: %#v", out.Warnings)
	}
}

// TS appendLegacyToolResultParts: non-image data files become inlineData plus
// "…returned this file as a response" (17d66c5).
func TestConvertToGoogleMessages_LegacyToolResultFileText(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID: "c1", ToolName: "read",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{Data: []byte("%PDF"), MediaType: "application/pdf"},
			types.FileContentBlock{Data: []byte{1}, MediaType: "image/png"},
		}},
	}}}}
	out, _ := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{SupportsFunctionResponseParts: false})
	parts := out.Contents[0]["parts"].([]map[string]interface{})
	if len(parts) != 4 {
		t.Fatalf("parts = %s", googleJSON(t, parts))
	}
	if parts[1]["text"] != "Tool executed successfully and returned this file as a response" {
		t.Errorf("file text = %v", parts[1]["text"])
	}
	if parts[3]["text"] != "Tool executed successfully and returned this image as a response" {
		t.Errorf("image text = %v", parts[3]["text"])
	}
}

// TestConvertToGoogleMessages_SupportedToolResultURLForwardedAsFileData
// ports TS "should convert supported tool result URLs into functionResponse
// file data" (ai@7.0.118 commit bc49f786f0): a tool-result file part whose
// URL matches SupportedFunctionResponseURLs (e.g. a gs:// URL on Vertex) is
// forwarded directly as functionResponse.parts[].fileData instead of being
// JSON-stringified as text.
func TestConvertToGoogleMessages_SupportedToolResultURLForwardedAsFileData(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID: "testCallId", ToolName: "imageGenerator",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{URL: "gs://example-bucket/renditions/hero.png", MediaType: "image/png"},
		}},
	}}}}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{
		IncludeFunctionCallIDs:        true,
		SupportsFunctionResponseParts: true,
		SupportedFunctionResponseURLs: map[string][]string{"*": {`^gs://.*$`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fr := out.Contents[0]["parts"].([]map[string]interface{})[0]["functionResponse"].(map[string]interface{})
	response := fr["response"].(map[string]interface{})
	if response["content"] != "Tool executed successfully." {
		t.Errorf("response.content = %v", response["content"])
	}
	respParts, ok := fr["parts"].([]map[string]interface{})
	if !ok || len(respParts) != 1 {
		t.Fatalf("functionResponse.parts = %#v", fr["parts"])
	}
	fileData, ok := respParts[0]["fileData"].(map[string]interface{})
	if !ok {
		t.Fatalf("parts[0] = %#v, want fileData", respParts[0])
	}
	if fileData["mimeType"] != "image/png" || fileData["fileUri"] != "gs://example-bucket/renditions/hero.png" {
		t.Errorf("fileData = %#v", fileData)
	}
}

// TestConvertToGoogleMessages_UnsupportedToolResultURLFallsBackToText verifies
// that without a matching SupportedFunctionResponseURLs entry (e.g. plain
// Google Developer API, which never sets it), the same gs:// URL falls back
// to the pre-existing JSON-stringified text behavior instead of fileData.
func TestConvertToGoogleMessages_UnsupportedToolResultURLFallsBackToText(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID: "testCallId", ToolName: "imageGenerator",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{URL: "gs://example-bucket/renditions/hero.png", MediaType: "image/png"},
		}},
	}}}}
	out, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{
		IncludeFunctionCallIDs:        true,
		SupportsFunctionResponseParts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fr := out.Contents[0]["parts"].([]map[string]interface{})[0]["functionResponse"].(map[string]interface{})
	if _, has := fr["parts"]; has {
		t.Fatalf("functionResponse.parts = %#v, want none (unsupported URL falls back to text)", fr["parts"])
	}
	response := fr["response"].(map[string]interface{})
	content, ok := response["content"].(string)
	if !ok || !strings.Contains(content, "gs://example-bucket/renditions/hero.png") {
		t.Errorf("response.content = %#v, want the JSON-stringified file part", response["content"])
	}
}

// TS: system messages become systemInstruction; Gemma folds them into the first user message.
func TestConvertToGoogleMessages_SystemAndGemma(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be nice"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	out, _ := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{})
	if got := googleJSON(t, out.SystemInstruction); got != `{"parts":[{"text":"be nice"}]}` {
		t.Errorf("systemInstruction = %s", got)
	}
	gemma, _ := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{IsGemmaModel: true})
	if gemma.SystemInstruction != nil {
		t.Error("gemma must not have systemInstruction")
	}
	if got := googleJSON(t, gemma.Contents[0]["parts"]); got != `[{"text":"be nice\n\n"},{"text":"hi"}]` {
		t.Errorf("gemma parts = %s", got)
	}
	_, err := ConvertToGoogleMessages([]types.Message{msgs[1], msgs[0]}, GoogleMessagesOptions{})
	if err == nil {
		t.Error("expected error for system message after user message")
	}
}

// TS readProviderOpts: Vertex reads googleVertex/vertex, falls back to google.
func TestConvertToGoogleMessages_VertexReadsThoughtSignatureKeys(t *testing.T) {
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "x", ProviderOptions: map[string]interface{}{key: map[string]interface{}{"thoughtSignature": "sig"}}},
		}}}
		out, _ := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{ProviderOptionsNames: []string{"googleVertex", "vertex"}})
		part := out.Contents[0]["parts"].([]map[string]interface{})[0]
		if !reflect.DeepEqual(part["thoughtSignature"], "sig") {
			t.Errorf("key %s: part = %#v", key, part)
		}
	}
}

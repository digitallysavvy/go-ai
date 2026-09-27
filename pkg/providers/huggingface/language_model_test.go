package huggingface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockIDGenerator returns a deterministic "id-0", "id-1", ... generator,
// mirroring TS's mockId() test helper.
func mockIDGenerator() func() string {
	n := 0
	return func() string {
		id := fmt.Sprintf("id-%d", n)
		n++
		return id
	}
}

func newTestModel(t *testing.T, handler http.HandlerFunc, modelID string) (*LanguageModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "APIKEY", BaseURL: srv.URL, GenerateID: mockIDGenerator()})
	return NewLanguageModel(p, modelID), srv
}

func jsonBody(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
}

var testPrompt = types.Prompt{
	Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	},
}

// Ported from "doGenerate > basic text response > should generate text".
func TestDoGenerateBasicText(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_67c97c0203188190a025beb4a75242bc","model":"deepseek-ai/DeepSeek-V3-0324",
			"object":"response","created_at":1741257730,"status":"completed","error":null,
			"instructions":null,"max_output_tokens":null,"metadata":null,"tool_choice":"auto",
			"tools":[],"temperature":1.0,"top_p":1.0,"incomplete_details":null,
			"usage":{"input_tokens":12,"output_tokens":25,"total_tokens":37},
			"output":[{"id":"msg_67c97c02656c81908e080dfdf4a03cd1","type":"message","role":"assistant",
			"status":"completed","content":[{"type":"output_text","text":"Hello! How can I help you today?"}]}],
			"output_text":"Hello! How can I help you today?"
		}`))
	}, "deepseek-ai/DeepSeek-V3-0324")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("Content = %#v, want 1 part", result.Content)
	}
	tc, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("Content[0] type = %T", result.Content[0])
	}
	if tc.Text != "Hello! How can I help you today?" {
		t.Fatalf("text = %q", tc.Text)
	}
	var meta map[string]map[string]string
	if err := json.Unmarshal(tc.ProviderMetadata, &meta); err != nil {
		t.Fatalf("providerMetadata: %v", err)
	}
	if meta["huggingface"]["itemId"] != "msg_67c97c02656c81908e080dfdf4a03cd1" {
		t.Fatalf("itemId = %#v", meta)
	}
}

// Ported from "doGenerate > basic text response > should extract usage".
func TestDoGenerateExtractUsage(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"usage":{"input_tokens":12,"output_tokens":25,"total_tokens":37},"output":[]}`))
	}, "m")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if *result.Usage.InputTokens != 12 || *result.Usage.OutputTokens != 25 || *result.Usage.TotalTokens != 37 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	if *result.Usage.InputDetails.NoCacheTokens != 12 || *result.Usage.InputDetails.CacheReadTokens != 0 {
		t.Fatalf("inputDetails = %#v", result.Usage.InputDetails)
	}
	if *result.Usage.OutputDetails.TextTokens != 25 || *result.Usage.OutputDetails.ReasoningTokens != 0 {
		t.Fatalf("outputDetails = %#v", result.Usage.OutputDetails)
	}
	assertJSONEqual(t, result.Usage.Raw, map[string]interface{}{
		"input_tokens": 12, "output_tokens": 25, "total_tokens": 37,
	})
}

// Ported from "doGenerate > basic text response > should handle missing usage gracefully".
func TestDoGenerateMissingUsage(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"usage":null,"output":[],"output_text":"Test response"}`))
	}, "m")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil || result.Usage.TotalTokens != nil || result.Usage.Raw != nil {
		t.Fatalf("usage = %#v, want all nil", result.Usage)
	}
}

// Ported from "doGenerate > basic text response > should send model id, settings, and input".
func TestDoGenerateSendsModelIDSettingsAndInput(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[]}`))
	}, "deepseek-ai/DeepSeek-V3-0324")

	temp := 0.5
	topP := 0.3
	maxTokens := 100
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		}},
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	want := map[string]interface{}{
		"model":             "deepseek-ai/DeepSeek-V3-0324",
		"temperature":       0.5,
		"top_p":             0.3,
		"max_output_tokens": float64(100),
		"stream":            false,
		"input": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are a helpful assistant."},
			map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "input_text", "text": "Hello"},
			}},
		},
	}
	assertJSONEqual(t, gotBody, want)
}

func assertJSONEqual(t *testing.T, got, want interface{}) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	var gotNorm, wantNorm interface{}
	_ = json.Unmarshal(gotJSON, &gotNorm)
	_ = json.Unmarshal(wantJSON, &wantNorm)
	gotStr, _ := json.Marshal(gotNorm)
	wantStr, _ := json.Marshal(wantNorm)
	if string(gotStr) != string(wantStr) {
		t.Fatalf("mismatch:\ngot  %s\nwant %s", gotStr, wantStr)
	}
}

// Ported from "doGenerate > basic text response > should handle unsupported settings with warnings".
func TestDoGenerateUnsupportedSettingsWarnings(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[]}`))
	}, "m")

	topK := 10
	seed := 123
	presence := 0.5
	frequency := 0.3
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:           testPrompt,
		TopK:             &topK,
		Seed:             &seed,
		PresencePenalty:  &presence,
		FrequencyPenalty: &frequency,
		StopSequences:    []string{"stop"},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	wantFeatures := []string{"topK", "seed", "presencePenalty", "frequencyPenalty", "stopSequences"}
	if len(result.Warnings) != len(wantFeatures) {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	for i, f := range wantFeatures {
		if result.Warnings[i].Type != "unsupported" || result.Warnings[i].Feature != f {
			t.Fatalf("warnings[%d] = %#v, want feature %q", i, result.Warnings[i], f)
		}
	}
}

// Ported from "doGenerate > basic text response > should generate text and sources from annotations".
func TestDoGenerateSourcesFromAnnotations(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"resp_test_annotations","model":"deepseek-ai/DeepSeek-V3-0324","created_at":1,
			"usage":{"input_tokens":20,"output_tokens":50,"total_tokens":70},
			"output":[{"id":"msg_test_annotations","type":"message","role":"assistant","content":[
				{"type":"output_text","text":"Here are some recent articles about AI.","annotations":[
					{"type":"url_citation","url":"https://example.com/article1","title":"AI Developments Article"},
					{"type":"url_citation","url":"https://test.com/article2","title":"Industry Trends Report"}
				]}
			]}]
		}`))
	}, "deepseek-ai/DeepSeek-V3-0324")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Content) != 3 {
		t.Fatalf("Content = %#v, want 3 parts", result.Content)
	}
	src1, ok := result.Content[1].(types.SourceContent)
	if !ok || src1.ID != "id-0" || src1.URL != "https://example.com/article1" || src1.Title != "AI Developments Article" || src1.SourceType != "url" {
		t.Fatalf("Content[1] = %#v", result.Content[1])
	}
	src2, ok := result.Content[2].(types.SourceContent)
	if !ok || src2.ID != "id-1" || src2.URL != "https://test.com/article2" {
		t.Fatalf("Content[2] = %#v", result.Content[2])
	}
}

// Ported from "doGenerate > basic text response > should handle MCP tools with annotations".
func TestDoGenerateMCPToolsWithAnnotations(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"resp_mcp_test","model":"m","created_at":1,
			"usage":{"input_tokens":50,"output_tokens":100,"total_tokens":150},
			"output":[
				{"id":"mcp_search_test","type":"mcp_call","server_label":"web_search","name":"search",
				 "arguments":"{\"query\": \"San Francisco tech events\"}","output":"Found 25 tech events in San Francisco"},
				{"id":"msg_mcp_response","type":"message","role":"assistant","content":[
					{"type":"output_text","text":"Based on the search results...","annotations":[
						{"type":"url_citation","url":"https://techevents.com/sf-ai","title":"SF AI Conference 2025"},
						{"type":"url_citation","url":"https://eventbrite.com/sf-startups","title":"SF Startup Meetups"}
					]}
				]}
			]
		}`))
	}, "m")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Content) != 5 {
		t.Fatalf("Content = %#v, want 5 parts", result.Content)
	}
	call, ok := result.Content[0].(types.ToolCallContent)
	if !ok || call.ToolCallID != "mcp_search_test" || call.ToolName != "search" || !call.ProviderExecuted {
		t.Fatalf("Content[0] = %#v", result.Content[0])
	}
	if call.Input != `{"query": "San Francisco tech events"}` {
		t.Fatalf("Input = %q", call.Input)
	}
	res, ok := result.Content[1].(types.ToolResultContent)
	if !ok || res.ToolCallID != "mcp_search_test" || res.Result != "Found 25 tech events in San Francisco" || res.ProviderExecuted {
		t.Fatalf("Content[1] = %#v", result.Content[1])
	}
	if _, ok := result.Content[2].(types.TextContent); !ok {
		t.Fatalf("Content[2] type = %T", result.Content[2])
	}
	if len(result.ToolCalls) != 1 || !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("ToolCalls = %#v", result.ToolCalls)
	}
}

// Ported from "doGenerate > tool calls > should handle function_call tool responses".
func TestDoGenerateFunctionCallToolResponses(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"resp_tool_test","model":"m","created_at":1,
			"usage":{"input_tokens":50,"output_tokens":30,"total_tokens":80},
			"output":[
				{"id":"fc_test","type":"function_call","call_id":"call_123","name":"getWeather",
				 "arguments":"{\"location\": \"New York\"}","output":"{\"temperature\": \"72°F\", \"condition\": \"sunny\"}"},
				{"id":"msg_after_tool","type":"message","role":"assistant","content":[
					{"type":"output_text","text":"The weather in New York is 72°F and sunny."}
				]}
			]
		}`))
	}, "m")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Content) != 3 {
		t.Fatalf("Content = %#v, want 3 parts", result.Content)
	}
	call, ok := result.Content[0].(types.ToolCallContent)
	if !ok || call.ProviderExecuted {
		t.Fatalf("Content[0] = %#v, want non-provider-executed tool-call", result.Content[0])
	}
	if call.ToolCallID != "call_123" || call.ToolName != "getWeather" {
		t.Fatalf("call = %#v", call)
	}
	res, ok := result.Content[1].(types.ToolResultContent)
	if !ok || res.ProviderExecuted {
		t.Fatalf("Content[1] = %#v, want non-provider-executed tool-result", result.Content[1])
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("ToolCalls = %#v", result.ToolCalls)
	}
	if result.ToolCalls[0].Arguments["location"] != "New York" {
		t.Fatalf("Arguments = %#v", result.ToolCalls[0].Arguments)
	}
}

// Ported from "message conversion > should convert user messages with images".
func TestDoGenerateConvertsUserImages(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test response"}`))
	}, "deepseek-ai/DeepSeek-V3-0324")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				types.TextContent{Text: "What do you see?"},
				types.FileContent{
					MediaType: "image/jpeg",
					FileData:  types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3, 4}, MediaType: "image/jpeg"},
				},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	input := gotBody["input"].([]interface{})
	content := input[0].(map[string]interface{})["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("content = %#v", content)
	}
	imgPart := content[1].(map[string]interface{})
	if imgPart["type"] != "input_image" || imgPart["image_url"] != "data:image/jpeg;base64,AQIDBA==" {
		t.Fatalf("image part = %#v", imgPart)
	}
}

// Ported from "message conversion > should throw for file parts with provider references".
func TestDoGenerateFileReferenceUnsupported(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called")
	}, "Qwen/Qwen2.5-VL-32B-Instruct")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{
					MediaType: "image/jpeg",
					FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"huggingface": "file-ref-123"}},
				},
			}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "file parts with provider references") {
		t.Fatalf("err = %v", err)
	}
}

// Ported from "message conversion > should handle assistant messages".
func TestDoGenerateAssistantMessages(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test response"}`))
	}, "deepseek-ai/DeepSeek-V3-0324")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi there!"}}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "How are you?"}}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	want := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "Hello"}}},
		map[string]interface{}{"role": "assistant", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "Hi there!"}}},
		map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "How are you?"}}},
	}
	assertJSONEqual(t, gotBody["input"], want)
}

// Ported from "message conversion > should warn about unsupported assistant content types".
func TestDoGenerateAssistantContentTypesNoWarnings(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test response"}`))
	}, "m")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "test", ToolName: "test"},
				types.ToolResultContent{ToolCallID: "test", ToolName: "test", Result: "test"},
				types.ReasoningContent{Text: "thinking..."},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
}

// Ported from "message conversion > should warn about tool messages".
func TestDoGenerateToolMessagesWarning(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test response"}`))
	}, "m")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "test", ToolName: "test", Result: "test"},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "tool messages" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// Ported from "structured output > should send text.format for structured output".
func TestDoGenerateStructuredOutputTextFormat(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[{"id":"msg","type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"name\":\"John\"}"}]}]}`))
	}, "moonshotai/Kimi-K2-Instruct")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: testPrompt,
		ResponseFormat: &provider.ResponseFormat{
			Type: "json",
			Schema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}, "age": map[string]interface{}{"type": "number"}},
				"required":   []interface{}{"name", "age"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	text, ok := gotBody["text"].(map[string]interface{})
	if !ok {
		t.Fatalf("text field missing: %#v", gotBody)
	}
	format := text["format"].(map[string]interface{})
	if format["type"] != "json_schema" || format["name"] != "response" || format["strict"] != false {
		t.Fatalf("format = %#v", format)
	}
	if _, hasDescription := format["description"]; hasDescription {
		t.Fatalf("description should be omitted: %#v", format)
	}
}

// Ported from "structured output > should handle structured output with custom name and description".
func TestDoGenerateStructuredOutputCustomNameDescription(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"{}"}`))
	}, "moonshotai/Kimi-K2-Instruct")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: testPrompt,
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json",
			Name:        "person_profile",
			Description: "A person profile with basic information",
			Schema:      map[string]interface{}{"type": "object"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	format := gotBody["text"].(map[string]interface{})["format"].(map[string]interface{})
	if format["name"] != "person_profile" || format["description"] != "A person profile with basic information" {
		t.Fatalf("format = %#v", format)
	}
}

// Ported from "reasoning > should handle reasoning content in responses".
func TestDoGenerateReasoningContent(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"resp_reasoning","model":"deepseek-ai/DeepSeek-R1","created_at":1,
			"usage":{"input_tokens":10,"output_tokens":50,"total_tokens":60},
			"output":[
				{"id":"reasoning_1","type":"reasoning","content":[{"type":"reasoning_text","text":"Let me think about this problem step by step..."}]},
				{"id":"msg_after_reasoning","type":"message","role":"assistant","content":[{"type":"output_text","text":"The answer is 42."}]}
			]
		}`))
	}, "deepseek-ai/DeepSeek-R1")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("Content = %#v", result.Content)
	}
	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok || rc.Text != "Let me think about this problem step by step..." {
		t.Fatalf("Content[0] = %#v", result.Content[0])
	}
	var meta map[string]map[string]string
	_ = json.Unmarshal(rc.ProviderMetadata, &meta)
	if meta["huggingface"]["itemId"] != "reasoning_1" {
		t.Fatalf("itemId = %#v", meta)
	}
	if _, ok := result.Content[1].(types.TextContent); !ok {
		t.Fatalf("Content[1] type = %T", result.Content[1])
	}
}

// Ported from "provider options > should send provider-specific options".
func TestDoGenerateProviderSpecificOptions(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test"}`))
	}, "deepseek-ai/DeepSeek-V3-0324")

	strict := true
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: testPrompt,
		ProviderOptions: map[string]interface{}{
			"huggingface": map[string]interface{}{
				"metadata":         map[string]interface{}{"key": "value"},
				"instructions":     "Be concise",
				"strictJsonSchema": strict,
			},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotBody["instructions"] != "Be concise" {
		t.Fatalf("instructions = %#v", gotBody["instructions"])
	}
	meta := gotBody["metadata"].(map[string]interface{})
	if meta["key"] != "value" {
		t.Fatalf("metadata = %#v", meta)
	}
	format := gotBody["text"].(map[string]interface{})["format"].(map[string]interface{})
	if format["strict"] != true {
		t.Fatalf("strict = %#v", format["strict"])
	}
}

// Ported from "tool preparation > should prepare tools correctly".
func TestDoGeneratePrepareTools(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test"}`))
	}, "m")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: testPrompt,
		Tools: []types.Tool{{
			Type:        types.ToolTypeFunction,
			Name:        "getWeather",
			Description: "Get weather information",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"location"},
			},
		}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "getWeather"},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	tools := gotBody["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" || tool["name"] != "getWeather" || tool["description"] != "Get weather information" {
		t.Fatalf("tool = %#v", tool)
	}
	toolChoice := gotBody["tool_choice"].(map[string]interface{})
	if toolChoice["type"] != "function" || toolChoice["function"].(map[string]interface{})["name"] != "getWeather" {
		t.Fatalf("tool_choice = %#v", toolChoice)
	}
}

// Ported from "tool preparation > should handle auto and required tool choices".
func TestDoGenerateAutoAndRequiredToolChoice(t *testing.T) {
	var gotBody map[string]interface{}
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = jsonBody(t, r)
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"output":[],"output_text":"Test"}`))
	}, "m")

	tools := []types.Tool{{Type: types.ToolTypeFunction, Name: "test", Parameters: map[string]interface{}{"type": "object"}}}

	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: testPrompt, Tools: tools, ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotBody["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %#v", gotBody["tool_choice"])
	}

	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: testPrompt, Tools: tools, ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotBody["tool_choice"] != "required" {
		t.Fatalf("tool_choice = %#v", gotBody["tool_choice"])
	}
}

// Ported from "top-level-only media type resolution" describe block.
func TestConvertToHuggingFaceResponsesInputMediaTypeResolution(t *testing.T) {
	pngBase64Bytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

	cases := []struct {
		name      string
		mediaType string
		fileData  types.FileData
		wantURL   string
	}{
		{
			name:      "full media type passes through unchanged",
			mediaType: "image/png",
			fileData:  types.FileData{Type: types.FileDataTypeData, Data: pngBase64Bytes},
			wantURL:   "data:image/png;base64,iVBORw0KGgo=",
		},
		{
			name:      "top-level-only detects subtype from bytes",
			mediaType: "image",
			fileData:  types.FileData{Type: types.FileDataTypeData, Data: pngBase64Bytes},
			wantURL:   "data:image/png;base64,iVBORw0KGgo=",
		},
		{
			name:      "wildcard normalizes via detection",
			mediaType: "image/*",
			fileData:  types.FileData{Type: types.FileDataTypeData, Data: pngBase64Bytes},
			wantURL:   "data:image/png;base64,iVBORw0KGgo=",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			part, err := hfConvertFileToImagePart(types.FileContent{MediaType: tc.mediaType, FileData: tc.fileData})
			if err != nil {
				t.Fatalf("hfConvertFileToImagePart: %v", err)
			}
			if part.ImageURL != tc.wantURL {
				t.Fatalf("ImageURL = %q, want %q", part.ImageURL, tc.wantURL)
			}
		})
	}

	t.Run("URL source passes through unchanged for top-level-only image", func(t *testing.T) {
		part, err := hfConvertFileToImagePart(types.FileContent{
			MediaType: "image",
			FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/x.png"},
		})
		if err != nil {
			t.Fatalf("hfConvertFileToImagePart: %v", err)
		}
		if part.ImageURL != "https://example.com/x.png" {
			t.Fatalf("ImageURL = %q", part.ImageURL)
		}
	})
}

// Not a TS-ported test: covers the huggingfaceFailedResponseHandler /
// huggingfaceErrorDataSchema wire-format mapping to *providererrors.ProviderError
// for a non-2xx HTTP response (huggingface-error.ts's createJsonErrorResponseHandler).
func TestDoGenerateHTTPErrorEnvelope(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error","code":"429"}}`))
	}, "m")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err == nil {
		t.Fatal("DoGenerate: expected error")
	}
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want 429", provErr.StatusCode)
	}
	if provErr.Message != "Rate limit exceeded" {
		t.Fatalf("Message = %q", provErr.Message)
	}
	if provErr.ErrorCode != "429" {
		t.Fatalf("ErrorCode = %q", provErr.ErrorCode)
	}
}

// Not a TS-ported test: a 200 response carrying a non-null top-level `error`
// field maps to a 400 ProviderError (TS doGenerate's `if (response.error)
// throw new APICallError(...)`).
func TestDoGenerateEmbeddedErrorField(t *testing.T) {
	model, _ := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","model":"m","created_at":1,"error":{"message":"Model overloaded"},"output":[]}`))
	}, "m")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: testPrompt})
	if err == nil {
		t.Fatal("DoGenerate: expected error")
	}
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.StatusCode != 400 || provErr.Message != "Model overloaded" {
		t.Fatalf("provErr = %#v", provErr)
	}
	if provErr.Retryable == nil || *provErr.Retryable {
		t.Fatalf("Retryable = %v, want false", provErr.Retryable)
	}
}

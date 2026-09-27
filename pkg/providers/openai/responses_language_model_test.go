package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// mockResponsesResponse returns a minimal valid Responses API response body.
func mockResponsesResponse(id, text string) responses.ResponsesAPIResponse {
	content, _ := json.Marshal(responses.AssistantMessageItem{
		Type: "message",
		Role: "assistant",
		Content: []responses.AssistantMessageContent{
			{Type: "output_text", Text: text},
		},
	})
	return responses.ResponsesAPIResponse{
		ID:     id,
		Model:  "gpt-4o",
		Output: []json.RawMessage{content},
		Usage: &responses.ResponsesAPIUsage{
			InputTokens:  5,
			OutputTokens: 10,
		},
	}
}

// TestResponsesLanguageModel_Provider verifies provider metadata.
func TestResponsesLanguageModel_Provider(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	if model.Provider() != "openai.responses" {
		t.Errorf("Provider() = %q, want %q", model.Provider(), "openai.responses")
	}
	if model.ModelID() != "gpt-4o" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "gpt-4o")
	}
	if !model.SupportsTools() {
		t.Error("SupportsTools() = false, want true")
	}
}

// TestResponsesLanguageModel_DoGenerate_Text verifies a basic text generation round-trip.
func TestResponsesLanguageModel_DoGenerate_Text(t *testing.T) {
	want := "Hello from Responses API!"
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("unexpected path %q, want /responses", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResponsesResponse("resp_test", want))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate failed: %v", err)
	}
	if result.Text != want {
		t.Errorf("Text = %q, want %q", result.Text, want)
	}
	// Verify request used "input" not "messages"
	if _, hasInput := capturedBody["input"]; !hasInput {
		t.Error("request body missing 'input' field")
	}
	if _, hasMessages := capturedBody["messages"]; hasMessages {
		t.Error("request body should not have 'messages' field")
	}
}

func TestResponsesLanguageModel_WebSearchIncludesSourcesAndMapsQueries(t *testing.T) {
	webSearchItem, _ := json.Marshal(WebSearchCallItem{
		Type:   "web_search_call",
		ID:     "ws_123",
		Status: "completed",
		Action: &WebSearchAction{
			Type:    "search",
			Queries: []string{"go generics", "type parameters"},
			Sources: []WebSearchSource{{Type: "url", URL: "https://go.dev/doc/"}},
		},
	})

	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "search"}}}}},
		Tools:  []types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{})},
	}
	body, _, err := model.buildRequestBody(opts, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		ID:     "resp_web",
		Model:  "gpt-4o",
		Output: []json.RawMessage{webSearchItem},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, responsesWebSearchToolName(opts.Tools))
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	include := body["include"].([]string)
	if include[0] != "web_search_call.action.sources" {
		t.Fatalf("include = %#v", include)
	}
	if len(result.ToolCalls) != 1 || !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content len = %d, want 2", len(result.Content))
	}
	toolResult, ok := result.Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("content[1] = %T", result.Content[1])
	}
	output := toolResult.Result.(map[string]interface{})
	action := output["action"].(map[string]interface{})
	queries := action["queries"].([]string)
	if queries[0] != "go generics" || queries[1] != "type parameters" {
		t.Fatalf("action = %#v", action)
	}
	sources := output["sources"].([]map[string]interface{})
	if sources[0]["url"] != "https://go.dev/doc/" {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestResponsesLanguageModel_WebSearchPreviewPreservesToolNameAndEmptyArrays(t *testing.T) {
	webSearchItem := json.RawMessage(`{"type":"web_search_call","id":"ws_preview","status":"completed","action":{"type":"search","queries":[],"sources":[]}}`)

	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")
	tools := []types.Tool{openaitool.WebSearchPreview(openaitool.WebSearchPreviewConfig{})}
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		ID:     "resp_web",
		Model:  "gpt-4o",
		Output: []json.RawMessage{webSearchItem},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, responsesWebSearchToolName(tools))
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if result.ToolCalls[0].ToolName != "web_search_preview" {
		t.Fatalf("tool name = %q, want web_search_preview", result.ToolCalls[0].ToolName)
	}
	toolResult := result.Content[1].(types.ToolResultContent)
	if toolResult.ToolName != "web_search_preview" {
		t.Fatalf("tool result name = %q, want web_search_preview", toolResult.ToolName)
	}
	output := toolResult.Result.(map[string]interface{})
	action := output["action"].(map[string]interface{})
	if queries, ok := action["queries"].([]string); !ok || len(queries) != 0 {
		t.Fatalf("queries = %#v, want empty []string", action["queries"])
	}
	if sources, ok := output["sources"].([]map[string]interface{}); !ok || len(sources) != 0 {
		t.Fatalf("sources = %#v, want empty []map", output["sources"])
	}
}

func TestResponsesLanguageModel_WebSearchToolChoiceUsesProviderType(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")
	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "search"}}}}},
		Tools:  []types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{})},
		ToolChoice: types.ToolChoice{
			Type:     types.ToolChoiceTool,
			ToolName: "web_search",
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	choice := body["tool_choice"].(map[string]interface{})
	if choice["type"] != "web_search" {
		t.Fatalf("tool_choice = %#v, want type web_search", choice)
	}
	if _, ok := choice["name"]; ok {
		t.Fatalf("tool_choice should not include name for web_search: %#v", choice)
	}
}

// TestResponsesLanguageModel_DoGenerate_SystemRole_Reasoning verifies that
// reasoning models receive "developer" role for system messages.
func TestResponsesLanguageModel_DoGenerate_SystemRole_Reasoning(t *testing.T) {
	tests := []struct {
		modelID      string
		expectedRole string
	}{
		{"o3", "developer"},
		{"gpt-5.4", "developer"},
		{"gpt-4o", "system"},
		{"gpt-5-chat-latest", "system"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			p := New(Config{APIKey: "test-key"})
			model := NewResponsesLanguageModel(p, tt.modelID)
			body, _, err := model.buildRequestBody(&provider.GenerateOptions{
				Prompt: types.Prompt{
					System: "You are a helpful assistant.",
					Messages: []types.Message{
						{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
					},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequestBody failed: %v", err)
			}
			inputRaw, ok := body["input"].([]interface{})
			if !ok || len(inputRaw) == 0 {
				t.Fatalf("input missing or empty")
			}
			// First item should be the system message.
			sysMsg, ok := inputRaw[0].(responses.SystemMessage)
			if !ok {
				t.Fatalf("first input item is %T, want responses.SystemMessage", inputRaw[0])
			}
			if sysMsg.Role != tt.expectedRole {
				t.Errorf("system message role = %q, want %q", sysMsg.Role, tt.expectedRole)
			}
		})
	}
}

// TestResponsesLanguageModel_DoGenerate_ToolCall verifies that function_call
// output items are converted to tool calls in the result.
func TestResponsesLanguageModel_DoGenerate_ToolCall(t *testing.T) {
	callItem, _ := json.Marshal(map[string]interface{}{
		"type":      "function_call",
		"id":        "fc_abc",
		"call_id":   "call_abc",
		"name":      "get_weather",
		"namespace": "weather_tools",
		"arguments": `{"location":"NYC"}`,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses.ResponsesAPIResponse{
			ID:     "resp_tool",
			Model:  "gpt-4o",
			Output: []json.RawMessage{callItem},
			Usage:  &responses.ResponsesAPIUsage{InputTokens: 5, OutputTokens: 5},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather?"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate failed: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolName != "get_weather" {
		t.Errorf("ToolName = %q, want %q", result.ToolCalls[0].ToolName, "get_weather")
	}
	if result.ToolCalls[0].Arguments["location"] != "NYC" {
		t.Errorf("location = %v, want NYC", result.ToolCalls[0].Arguments["location"])
	}
	openaiMeta, ok := result.ToolCalls[0].ProviderMetadata["openai"].(map[string]interface{})
	if !ok {
		t.Fatalf("openai provider metadata missing: %#v", result.ToolCalls[0].ProviderMetadata)
	}
	if openaiMeta["itemId"] != "fc_abc" {
		t.Errorf("itemId = %v, want fc_abc", openaiMeta["itemId"])
	}
	if openaiMeta["namespace"] != "weather_tools" {
		t.Errorf("namespace = %v, want weather_tools", openaiMeta["namespace"])
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("FinishReason = %q, want tool-calls", result.FinishReason)
	}
}

func TestResponsesLanguageModel_AllowedToolsProviderOption(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather?"}}},
		}},
		Tools: []types.Tool{
			{Name: "weather", Description: "Get weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
			{Name: "cityAttractions", Description: "Find attractions", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedTools": map[string]interface{}{
					"toolNames": []interface{}{"weather"},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}

	if tools, ok := body["tools"].([]interface{}); !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	choice, ok := body["tool_choice"].(responses.AllowedToolsToolChoice)
	if !ok {
		t.Fatalf("tool_choice = %T %#v", body["tool_choice"], body["tool_choice"])
	}
	if choice.Type != "allowed_tools" || choice.Mode != "auto" || len(choice.Tools) != 1 || choice.Tools[0].Name != "weather" {
		t.Fatalf("tool_choice = %#v", choice)
	}
}

// TestResponsesLanguageModel_AllowedToolsResolvesBuiltinToolType covers row
// a062795: an allowedTools entry naming a built-in provider tool must
// resolve to that tool's own type (e.g. `{type:"web_search"}`), never to
// `{type:"function", name:...}` — the API rejects the latter for a tool that
// isn't actually a function.
func TestResponsesLanguageModel_AllowedToolsResolvesBuiltinToolType(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Search?"}}},
		}},
		Tools: []types.Tool{{
			Type:       types.ToolTypeProviderDefined,
			Name:       "browser_search",
			ProviderID: "openai.web_search",
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedTools": map[string]interface{}{
					"toolNames": []string{"browser_search"},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	choice := body["tool_choice"].(responses.AllowedToolsToolChoice)
	if choice.Tools[0].Type != "web_search" || choice.Tools[0].Name != "" {
		t.Fatalf("allowed tool entry = %#v, want built-in web_search entry without a name", choice.Tools[0])
	}
}

// TestResponsesLanguageModel_AllowedToolsFunctionAndUnknown covers the
// function and "not part of this request" branches of a062795.
func TestResponsesLanguageModel_AllowedToolsFunctionAndUnknown(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		Tools: []types.Tool{{
			Type: types.ToolTypeFunction,
			Name: "lookup",
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedTools": map[string]interface{}{
					"toolNames": []string{"lookup", "not_in_request"},
					"mode":      "required",
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	choice := body["tool_choice"].(responses.AllowedToolsToolChoice)
	if choice.Mode != "required" || len(choice.Tools) != 2 {
		t.Fatalf("allowed tools choice = %#v", choice)
	}
	if choice.Tools[0].Type != "function" || choice.Tools[0].Name != "lookup" {
		t.Fatalf("allowed tool[0] = %#v, want function lookup", choice.Tools[0])
	}
	if choice.Tools[1].Type != "function" || choice.Tools[1].Name != "not_in_request" {
		t.Fatalf("allowed tool[1] = %#v, want unknown name sent through as function", choice.Tools[1])
	}
	if len(warnings) != 1 || warnings[0].Feature != `allowedTools entry "not_in_request"` {
		t.Fatalf("warnings = %#v, want unknown allowedTools entry warning", warnings)
	}
}

// TestResponsesLanguageModel_AllowedToolsAllDroppedErrors covers the
// UnsupportedFunctionalityError path: when every requested tool name is
// unsupported for allow-listing, buildRequestBody must return an error.
func TestResponsesLanguageModel_AllowedToolsAllDroppedErrors(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	_, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		Tools: []types.Tool{{
			Type: types.ToolTypeFunction,
			Name: "deferred_tool",
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{"deferLoading": true},
			},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedTools": map[string]interface{}{
					"toolNames": []string{"deferred_tool"},
				},
			},
		},
	}, false)
	if err == nil {
		t.Fatal("expected an error when every allowedTools entry is dropped")
	}
}

func TestResponsesLanguageModel_WebSearchProviderIDUsesCallerToolName(t *testing.T) {
	webSearchItem, _ := json.Marshal(WebSearchCallItem{
		Type:   "web_search_call",
		ID:     "ws_custom",
		Status: "completed",
		Action: &WebSearchAction{Type: "search", Queries: []string{"go"}},
	})

	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")
	tools := []types.Tool{{
		Type:       types.ToolTypeProviderDefined,
		Name:       "browser_search",
		ProviderID: "openai.web_search",
	}}
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		ID:     "resp_web",
		Model:  "gpt-4o",
		Output: []json.RawMessage{webSearchItem},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, responsesWebSearchToolName(tools))
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if result.ToolCalls[0].ToolName != "browser_search" {
		t.Fatalf("tool call name = %q, want caller-facing browser_search", result.ToolCalls[0].ToolName)
	}
	toolResult := result.Content[1].(types.ToolResultContent)
	if toolResult.ToolName != "browser_search" {
		t.Fatalf("tool result name = %q, want caller-facing browser_search", toolResult.ToolName)
	}
}

func TestResponsesLanguageModel_HasToolMatchesProviderID(t *testing.T) {
	tools := []types.Tool{{
		Type:       types.ToolTypeProviderDefined,
		ProviderID: "openai.shell",
	}}
	if !hasTool(tools, "openai.shell") {
		t.Fatal("hasTool should match provider-defined tools by ProviderID")
	}
}

func TestResponsesLanguageModel_AllowedToolsRequiredMode(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather?"}}},
		}},
		Tools: []types.Tool{{Name: "weather"}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedTools": map[string]interface{}{
					"toolNames": []string{"weather"},
					"mode":      "required",
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	choice := body["tool_choice"].(responses.AllowedToolsToolChoice)
	if choice.Mode != "required" {
		t.Fatalf("mode = %q", choice.Mode)
	}
}

// TestResponsesLanguageModel_DoStream_Text verifies text streaming via SSE.
func TestResponsesLanguageModel_DoStream_Text(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`{"type":"response.created","response":{"id":"resp_stream","model":"gpt-4o"}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}`,
			`{"type":"response.output_text.delta","output_index":0,"delta":"Hello"}`,
			`{"type":"response.output_text.delta","output_index":0,"delta":" world"}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`,
			`{"type":"response.completed","response":{"id":"resp_stream","usage":{"input_tokens":5,"output_tokens":3}}}`,
		}
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var textChunks []string
	var finishChunk *provider.StreamChunk

	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		switch chunk.Type {
		case provider.ChunkTypeText:
			textChunks = append(textChunks, chunk.Text)
		case provider.ChunkTypeFinish:
			finishChunk = chunk
		}
	}

	text := strings.Join(textChunks, "")
	if text != "Hello world" {
		t.Errorf("streamed text = %q, want %q", text, "Hello world")
	}
	if finishChunk == nil {
		t.Error("expected a finish chunk")
	} else if finishChunk.FinishReason != types.FinishReasonStop {
		t.Errorf("FinishReason = %q, want stop", finishChunk.FinishReason)
	}
}

func TestResponsesLanguageModel_DoStreamIncludesRawChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"resp_1","created_at":1702657020,"model":"gpt-4o"}}

data: {"type":"response.output_text.delta","delta":"Hello"}

data: {"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

data: [DONE]

`)
	}))
	defer server.Close()

	model := NewResponsesLanguageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "gpt-4o")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hello"},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatalf("DoStream() error: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeStreamStart {
		t.Fatalf("first chunk type = %v, want stream-start", chunk.Type)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("second chunk type = %v, want raw", chunk.Type)
	}
	raw, ok := chunk.Raw.(map[string]interface{})
	if !ok || raw["type"] != "response.created" {
		t.Fatalf("raw chunk = %#v, want response.created", chunk.Raw)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("third chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("third chunk type = %v, want response-metadata", chunk.Type)
	}
	if chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "resp_1" || chunk.ResponseMetadata.ModelID != "gpt-4o" {
		t.Fatalf("response metadata = %#v, want response.created metadata", chunk.ResponseMetadata)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("fourth chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("fourth chunk type = %v, want raw", chunk.Type)
	}
	raw, ok = chunk.Raw.(map[string]interface{})
	if !ok || raw["type"] != "response.output_text.delta" {
		t.Fatalf("raw chunk = %#v, want response.output_text.delta", chunk.Raw)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("fifth chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
		t.Fatalf("fifth chunk = %#v, want text Hello", chunk)
	}
}

func TestResponsesLanguageModel_DoStreamParseErrorEmitsErrorChunkAfterRaw(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":

data: [DONE]

`)), true)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("second chunk type = %v, want error", chunk.Type)
	}
	if !strings.Contains(chunk.Text, "failed to parse stream chunk") {
		t.Fatalf("error text = %q, want parse failure", chunk.Text)
	}
}

func TestResponsesLanguageModel_DoStreamEarlyProviderErrorReturnsError(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_error","created_at":1741269019,"model":"gpt-4o"}}

data: {"type":"error","code":"rate_limit_exceeded","message":"boom"}

data: [DONE]

`)), true)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err == nil {
		t.Fatalf("stream.Next error = nil, chunk = %#v", chunk)
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %[1]v, want ProviderError", err)
	}
	if providerErr.StatusCode != 429 || providerErr.Message != "boom" || providerErr.ResponseBody == "" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}

func TestResponsesLanguageModel_DoStreamEarlyResponseFailedReturnsError(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_failed","created_at":1741269019,"model":"gpt-4o"}}

data: {"type":"response.failed","sequence_number":1,"response":{"error":{"code":"server_error","message":"response failed"},"incomplete_details":null,"usage":null,"service_tier":null}}

`)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err == nil {
		t.Fatalf("stream.Next error = nil, chunk = %#v", chunk)
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %[1]v, want ProviderError", err)
	}
	if providerErr.StatusCode != 500 || providerErr.Message != "response failed" {
		t.Fatalf("provider error = %#v", providerErr)
	}
}

func TestResponsesLanguageModel_DoStreamIncompleteUsesCreatedResponseID(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.created","response":{"id":"resp_created","created_at":1741269019,"model":"gpt-4o"}}

data: {"type":"response.incomplete","response":{"id":"resp_terminal","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":0,"output_tokens":0}}}

`)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("metadata chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata || chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "resp_created" {
		t.Fatalf("metadata chunk = %#v, want response metadata for resp_created", chunk)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("finish chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeFinish || chunk.FinishReason != types.FinishReasonLength {
		t.Fatalf("finish chunk = %#v, want length finish", chunk)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(chunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("provider metadata unmarshal failed: %v", err)
	}
	openaiMeta, ok := meta["openai"].(map[string]interface{})
	if !ok || openaiMeta["responseId"] != "resp_created" {
		t.Fatalf("provider metadata = %#v, want created response id", meta)
	}
}

// TestResponsesLanguageModel_DoStream_ToolCall verifies tool call accumulation in streaming.
func TestResponsesLanguageModel_DoStream_ToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		callDoneItem, _ := json.Marshal(map[string]interface{}{
			"type":      "function_call",
			"id":        "fc_xyz",
			"call_id":   "call_xyz",
			"name":      "search",
			"namespace": "search_tools",
			"arguments": `{"query":"go lang"}`,
		})
		events := []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_xyz","call_id":"call_xyz","name":"search","namespace":"search_tools"}}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"query\":"}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"go lang\"}"}`,
			fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, string(callDoneItem)),
			`{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":5,"output_tokens":5}}}`,
		}
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Search"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var toolChunk *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			toolChunk = chunk
		}
	}

	if toolChunk == nil {
		t.Fatal("expected tool call chunk")
	}
	if toolChunk.ToolCall.ToolName != "search" {
		t.Errorf("ToolName = %q, want %q", toolChunk.ToolCall.ToolName, "search")
	}
	if toolChunk.ToolCall.Arguments["query"] != "go lang" {
		t.Errorf("query = %v, want %q", toolChunk.ToolCall.Arguments["query"], "go lang")
	}
	openaiMeta, ok := toolChunk.ToolCall.ProviderMetadata["openai"].(map[string]interface{})
	if !ok {
		t.Fatalf("openai provider metadata missing: %#v", toolChunk.ToolCall.ProviderMetadata)
	}
	if openaiMeta["itemId"] != "fc_xyz" {
		t.Errorf("itemId = %v, want fc_xyz", openaiMeta["itemId"])
	}
	if openaiMeta["namespace"] != "search_tools" {
		t.Errorf("namespace = %v, want search_tools", openaiMeta["namespace"])
	}
}

func TestResponsesLanguageModel_DoStream_WebSearchCallLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		doneItem, _ := json.Marshal(WebSearchCallItem{
			Type:   "web_search_call",
			ID:     "ws_123",
			Status: "completed",
			Action: &WebSearchAction{
				Type:    "search",
				Queries: []string{"go ai sdk"},
			},
		})
		events := []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"web_search_call","id":"ws_123","status":"in_progress"}}`,
			fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, string(doneItem)),
			`{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":5,"output_tokens":5}}}`,
		}
		for _, e := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Search"}}},
			},
		},
		Tools: []types.Tool{openaitool.WebSearchPreview(openaitool.WebSearchPreviewConfig{})},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var got []provider.ChunkType
	var toolCall *types.ToolCall
	var toolResult *types.ToolResult
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeStreamStart {
			continue
		}
		got = append(got, chunk.Type)
		switch chunk.Type {
		case provider.ChunkTypeToolCall:
			toolCall = chunk.ToolCall
		case provider.ChunkTypeToolResult:
			toolResult = chunk.ToolResult
		}
	}

	wantPrefix := []provider.ChunkType{
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeToolResult,
	}
	if len(got) < len(wantPrefix) {
		t.Fatalf("chunks = %v, want prefix %v", got, wantPrefix)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("chunks = %v, want prefix %v", got, wantPrefix)
		}
	}
	if toolCall == nil || toolCall.ToolName != "web_search_preview" || !toolCall.ProviderExecuted {
		t.Fatalf("tool call = %#v, want provider-executed web_search_preview", toolCall)
	}
	if toolResult == nil || toolResult.ToolName != "web_search_preview" {
		t.Fatalf("tool result = %#v, want web_search_preview", toolResult)
	}
	output := toolResult.Result.(map[string]interface{})
	action := output["action"].(map[string]interface{})
	queries := action["queries"].([]string)
	if len(queries) != 1 || queries[0] != "go ai sdk" {
		t.Fatalf("queries = %#v", action["queries"])
	}
}

// TestResponsesLanguageModel_DoGenerate_StoreOff_Include verifies that store=false
// on a reasoning model automatically adds "reasoning.encrypted_content" to include.
func TestResponsesLanguageModel_DoGenerate_StoreOff_Include(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "o3")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"store": false},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}

	include, ok := body["include"].([]string)
	if !ok {
		t.Fatal("expected include field in body")
	}
	found := false
	for _, v := range include {
		if v == "reasoning.encrypted_content" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'reasoning.encrypted_content' in include, got %v", include)
	}
}

// TestResponsesLanguageModel_PreviousResponseId verifies that previousResponseId
// is forwarded in the request body.
func TestResponsesLanguageModel_PreviousResponseId(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "continue"}}},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"previousResponseId": "resp_prev123"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	if body["previous_response_id"] != "resp_prev123" {
		t.Errorf("previous_response_id = %v, want resp_prev123", body["previous_response_id"])
	}
}

func TestResponsesLanguageModel_ConversationProviderOption(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "continue"}}},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"conversation": "conv_123"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	if body["conversation"] != "conv_123" {
		t.Errorf("conversation = %v, want conv_123", body["conversation"])
	}
}

func TestResponsesLanguageModel_ConversationSkipsStoredReasoningAndWarnsWithPreviousResponseID(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-5")

	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ReasoningContent{
							Text:             "summary",
							EncryptedContent: "enc_123",
							ProviderOptions: map[string]interface{}{
								"openai": map[string]interface{}{"itemId": "rs_123"},
							},
						},
						types.TextContent{Text: "answer"},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"conversation":       "conv_123",
				"previousResponseId": "resp_prev123",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if body["conversation"] != "conv_123" || body["previous_response_id"] != "resp_prev123" {
		t.Fatalf("conversation fields missing from body: %#v", body)
	}
	input := body["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input length = %d, want only assistant message", len(input))
	}
	msg := input[0].(map[string]interface{})
	if msg["role"] != "assistant" || msg["content"] != "answer" || msg["type"] != nil {
		t.Fatalf("assistant message item = %#v, want easy input message without id/type", msg)
	}
	if len(warnings) != 1 || warnings[0].Feature != "conversation" {
		t.Fatalf("warnings = %#v, want conversation warning", warnings)
	}
}

// TestResponsesLanguageModel_PreviousResponseIdKeepsPlainFunctionCallsInFull
// verifies that plain client-executed function calls are always resent in
// full when chaining with previousResponseId, never as an item_reference.
// Only provider-defined tool calls (local_shell/shell/apply_patch/computer/
// custom) may be reduced to an item_reference in that case. See TS
// convert-to-openai-responses-input.ts (row d302134): sending an
// item_reference for a plain function call breaks call/output pairing
// because function_call_output can only reference by call_id.
func TestResponsesLanguageModel_PreviousResponseIdKeepsPlainFunctionCallsInFull(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleAssistant,
					ToolCalls: []types.ToolCall{
						{
							ID:        "call_1",
							ToolName:  "lookup",
							Arguments: map[string]interface{}{"q": "go"},
							ProviderMetadata: map[string]interface{}{
								"openai": map[string]interface{}{"itemId": "fc_123"},
							},
						},
						{
							ID:        "call_2",
							ToolName:  "fresh",
							Arguments: map[string]interface{}{"q": "new"},
						},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"previousResponseId": "resp_prev123"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input := body["input"].([]interface{})
	if len(input) != 2 {
		t.Fatalf("input length = %d, want both plain function calls resent in full: %#v", len(input), input)
	}
	first := input[0].(responses.FunctionCallItem)
	if first.CallID != "call_1" || first.Name != "lookup" || first.ID != "" {
		t.Fatalf("unexpected first function call item: %#v", first)
	}
	second := input[1].(responses.FunctionCallItem)
	if second.CallID != "call_2" || second.Name != "fresh" {
		t.Fatalf("unexpected second function call item: %#v", second)
	}
}

func TestResponsesLanguageModel_PreviousResponseIdSkipsStoredReasoning(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-5")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ReasoningContent{
							Text:             "summary",
							EncryptedContent: "enc_123",
							ProviderOptions: map[string]interface{}{
								"openai": map[string]interface{}{"itemId": "rs_123"},
							},
						},
						types.TextContent{Text: "answer"},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"previousResponseId": "resp_prev123"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input := body["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input length = %d, want only assistant message", len(input))
	}
	msg := input[0].(map[string]interface{})
	if msg["role"] != "assistant" || msg["content"] != "answer" || msg["type"] != nil {
		t.Fatalf("assistant message item = %#v, want easy input message without id/type", msg)
	}
}

func TestResponsesLanguageModel_StoreUsesReasoningItemReference(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-5")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ReasoningContent{
							Text:             "summary",
							EncryptedContent: "enc_123",
							ProviderOptions: map[string]interface{}{
								"openai": map[string]interface{}{"itemId": "rs_123"},
							},
						},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"store": true},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input := body["input"].([]interface{})
	item := input[0].(map[string]interface{})
	if item["type"] != "item_reference" || item["id"] != "rs_123" {
		t.Fatalf("reasoning item = %#v, want item_reference rs_123", item)
	}
}

func TestResponsesLanguageModel_MessageItemMetadataRoundTrips(t *testing.T) {
	phase := "final_answer"
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	raw, _ := json.Marshal(responses.AssistantMessageItem{
		Type:  "message",
		Role:  "assistant",
		ID:    "msg_123",
		Phase: &phase,
		Content: []responses.AssistantMessageContent{
			{Type: "output_text", Text: "stored answer"},
		},
	})
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		ID:     "resp_123",
		Model:  "gpt-4o",
		Output: []json.RawMessage{raw},
	}, true, "")
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	text, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want TextContent", result.Content[0])
	}
	openaiMeta := text.ProviderOptions["openai"].(map[string]interface{})
	if openaiMeta["itemId"] != "msg_123" || openaiMeta["phase"] != "final_answer" {
		t.Fatalf("message provider options = %#v", text.ProviderOptions)
	}
	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role:    types.RoleAssistant,
			Content: result.Content,
		}}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"store": true},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input := body["input"].([]interface{})
	item := input[0].(map[string]interface{})
	if item["type"] != "item_reference" || item["id"] != "msg_123" {
		t.Fatalf("round-tripped message item = %#v, want item_reference msg_123", item)
	}
}

// TestResponsesLanguageModel_TextVerbosity verifies that textVerbosity is mapped
// to text.verbosity in the Responses API request body.
func TestResponsesLanguageModel_TextVerbosity(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-5-mini")

	tests := []struct {
		name      string
		verbosity string
	}{
		{"low", "low"},
		{"medium", "medium"},
		{"high", "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _, err := model.buildRequestBody(&provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
					},
				},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"textVerbosity": tt.verbosity},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequestBody failed: %v", err)
			}

			textObj, ok := body["text"].(map[string]interface{})
			if !ok {
				t.Fatal("expected text object in body")
			}
			v, ok := textObj["verbosity"].(string)
			if !ok {
				t.Fatalf("expected verbosity in text object, got %v", textObj)
			}
			if v != tt.verbosity {
				t.Errorf("text.verbosity = %q, want %q", v, tt.verbosity)
			}
		})
	}
}

// TestResponsesLanguageModel_TextVerbosityWithFormat verifies that textVerbosity
// and responseFormat coexist correctly in the text object.
func TestResponsesLanguageModel_TextVerbosityWithFormat(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-5-mini")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"textVerbosity": "low"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}

	textObj, ok := body["text"].(map[string]interface{})
	if !ok {
		t.Fatal("expected text object in body")
	}
	if textObj["verbosity"] != "low" {
		t.Errorf("text.verbosity = %v, want %q", textObj["verbosity"], "low")
	}
	if _, ok := textObj["format"]; !ok {
		t.Error("expected format in text object when responseFormat is set")
	}
}

// TestResponsesLanguageModel_NoTextVerbosity verifies that the text object is not
// present when neither textVerbosity nor responseFormat is set.
func TestResponsesLanguageModel_NoTextVerbosity(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-5-mini")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}

	if _, ok := body["text"]; ok {
		t.Error("expected no text object when textVerbosity and responseFormat are not set")
	}
}

func TestResponsesLanguageModel_ResponsesRequestParityOptions(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 40
	seed := 123
	presencePenalty := 0.2
	frequencyPenalty := 0.3

	tests := []struct {
		name          string
		modelID       string
		opts          provider.GenerateOptions
		stream        bool
		assertBody    func(t *testing.T, body map[string]interface{})
		assertWarning func(t *testing.T, warnings []types.Warning)
	}{
		{
			name:    "non-stream requests omit stream false",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{Prompt: types.Prompt{
				Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
			}},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if _, ok := body["stream"]; ok {
					t.Fatalf("stream = %#v, want omitted for doGenerate parity", body["stream"])
				}
			},
		},
		{
			name:    "stream requests set stream true",
			modelID: "gpt-4o",
			stream:  true,
			opts: provider.GenerateOptions{Prompt: types.Prompt{
				Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
			}},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["stream"] != true {
					t.Fatalf("stream = %#v, want true", body["stream"])
				}
			},
		},
		{
			name:    "text response format does not create text object",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{
				Prompt:         types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ResponseFormat: &provider.ResponseFormat{Type: "text"},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if _, ok := body["text"]; ok {
					t.Fatalf("text = %#v, want omitted for responseFormat text", body["text"])
				}
			},
		},
		{
			name:    "json response format without schema uses json_object",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{
				Prompt:         types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ResponseFormat: &provider.ResponseFormat{Type: "json"},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				textObj := body["text"].(map[string]interface{})
				format := textObj["format"].(map[string]interface{})
				if format["type"] != "json_object" {
					t.Fatalf("text.format = %#v, want json_object", format)
				}
			},
		},
		{
			name:    "json schema honors strictJsonSchema false name description and verbosity",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ResponseFormat: &provider.ResponseFormat{
					Type:        "json",
					Name:        "weather",
					Description: "weather answer",
					Schema: map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
					},
				},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"strictJsonSchema": false, "textVerbosity": "high"},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				textObj := body["text"].(map[string]interface{})
				if textObj["verbosity"] != "high" {
					t.Fatalf("text.verbosity = %#v, want high", textObj["verbosity"])
				}
				format := textObj["format"].(map[string]interface{})
				if format["type"] != "json_schema" || format["strict"] != false || format["name"] != "weather" || format["description"] != "weather answer" {
					t.Fatalf("text.format = %#v", format)
				}
			},
		},
		{
			name:    "reasoning effort defaults summary to detailed",
			modelID: "gpt-5.1-codex-max",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "xhigh"},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				reasoning := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "xhigh" || reasoning["summary"] != "detailed" {
					t.Fatalf("reasoning = %#v, want xhigh/detailed", reasoning)
				}
			},
			assertWarning: func(t *testing.T, warnings []types.Warning) {
				assertNoWarning(t, warnings, "reasoningEffort")
			},
		},
		{
			name:    "top-level reasoning defaults summary to detailed",
			modelID: "o3-mini",
			opts: provider.GenerateOptions{
				Prompt:    types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				Reasoning: ptrReasoning(types.ReasoningMedium),
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				reasoning := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "medium" || reasoning["summary"] != "detailed" {
					t.Fatalf("reasoning = %#v, want medium/detailed", reasoning)
				}
			},
		},
		{
			name:    "reasoning none does not default summary",
			modelID: "gpt-5.2",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "none"},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				reasoning := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "none" {
					t.Fatalf("reasoning effort = %#v, want none", reasoning["effort"])
				}
				if _, ok := reasoning["summary"]; ok {
					t.Fatalf("reasoning summary = %#v, want omitted", reasoning["summary"])
				}
			},
		},
		{
			name:    "reasoning summary null suppresses default summary like TypeScript",
			modelID: "o3-mini",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "low", "reasoningSummary": nil},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				reasoning := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "low" {
					t.Fatalf("reasoning effort = %#v, want low", reasoning["effort"])
				}
				if _, ok := reasoning["summary"]; ok {
					t.Fatalf("reasoning summary = %#v, want omitted for explicit null", reasoning["summary"])
				}
			},
			assertWarning: func(t *testing.T, warnings []types.Warning) {
				assertNoWarning(t, warnings, "reasoningSummary")
			},
		},
		{
			name:    "non-reasoning model warns and omits provider reasoning",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "high", "reasoningSummary": "auto"},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if _, ok := body["reasoning"]; ok {
					t.Fatalf("reasoning = %#v, want omitted for non-reasoning model", body["reasoning"])
				}
			},
			assertWarning: func(t *testing.T, warnings []types.Warning) {
				assertHasWarning(t, warnings, "reasoningEffort")
				assertHasWarning(t, warnings, "reasoningSummary")
			},
		},
		{
			name:    "reasoning model removes sampling unless effort none is supported",
			modelID: "o3",
			opts: provider.GenerateOptions{
				Prompt:      types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				Temperature: &temp,
				TopP:        &topP,
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if _, ok := body["temperature"]; ok {
					t.Fatalf("temperature = %#v, want omitted", body["temperature"])
				}
				if _, ok := body["top_p"]; ok {
					t.Fatalf("top_p = %#v, want omitted", body["top_p"])
				}
			},
			assertWarning: func(t *testing.T, warnings []types.Warning) {
				assertHasWarning(t, warnings, "temperature")
				assertHasWarning(t, warnings, "topP")
			},
		},
		{
			name:    "gpt-5.2 reasoning none keeps sampling",
			modelID: "gpt-5.2",
			opts: provider.GenerateOptions{
				Prompt:      types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				Temperature: &temp,
				TopP:        &topP,
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "none"},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["temperature"] != temp || body["top_p"] != topP {
					t.Fatalf("sampling = %#v/%#v, want preserved", body["temperature"], body["top_p"])
				}
			},
			assertWarning: func(t *testing.T, warnings []types.Warning) {
				assertNoWarning(t, warnings, "temperature")
				assertNoWarning(t, warnings, "topP")
			},
		},
		{
			name:    "unsupported top-level settings warn and omit",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{
				Prompt:           types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				TopK:             &topK,
				Seed:             &seed,
				PresencePenalty:  &presencePenalty,
				FrequencyPenalty: &frequencyPenalty,
				StopSequences:    []string{},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for _, key := range []string{"top_k", "seed", "presence_penalty", "frequency_penalty", "stop"} {
					if _, ok := body[key]; ok {
						t.Fatalf("%s = %#v, want omitted", key, body[key])
					}
				}
			},
			assertWarning: func(t *testing.T, warnings []types.Warning) {
				for _, feature := range []string{"topK", "seed", "presencePenalty", "frequencyPenalty", "stopSequences"} {
					assertHasWarning(t, warnings, feature)
				}
			},
		},
		{
			name:    "provider options preserve nulls zeroes and derived includes",
			modelID: "o3",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				Tools:  []types.Tool{openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{})},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{
						"conversation":       nil,
						"metadata":           nil,
						"store":              false,
						"previousResponseId": nil,
						"promptCacheKey":     "cache-key",
						"serviceTier":        nil,
						"parallelToolCalls":  nil,
						"maxToolCalls":       0,
						"logprobs":           5,
						"contextManagement":  []interface{}{map[string]interface{}{"type": "auto", "compactThreshold": 0.75}},
					},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if _, ok := body["conversation"]; !ok || body["conversation"] != nil {
					t.Fatalf("conversation = %#v, want explicit nil", body["conversation"])
				}
				if _, ok := body["metadata"]; !ok || body["metadata"] != nil {
					t.Fatalf("metadata = %#v, want explicit nil", body["metadata"])
				}
				if body["store"] != false {
					t.Fatalf("store = %#v, want false", body["store"])
				}
				if body["prompt_cache_key"] != "cache-key" {
					t.Fatalf("prompt_cache_key = %#v", body["prompt_cache_key"])
				}
				if body["max_tool_calls"] != 0 {
					t.Fatalf("max_tool_calls = %#v, want 0", body["max_tool_calls"])
				}
				if body["top_logprobs"] != 5 {
					t.Fatalf("top_logprobs = %#v, want 5", body["top_logprobs"])
				}
				includes := body["include"].([]string)
				for _, include := range []string{"reasoning.encrypted_content", "code_interpreter_call.outputs", "message.output_text.logprobs"} {
					if !containsString(includes, include) {
						t.Fatalf("include = %#v, missing %s", includes, include)
					}
				}
				cm := body["context_management"].([]map[string]interface{})
				if cm[0]["compact_threshold"] != 0.75 {
					t.Fatalf("context_management = %#v", cm)
				}
				if _, ok := body["service_tier"]; !ok || body["service_tier"] != nil {
					t.Fatalf("service_tier = %#v, want explicit nil", body["service_tier"])
				}
				if _, ok := body["parallel_tool_calls"]; !ok || body["parallel_tool_calls"] != nil {
					t.Fatalf("parallel_tool_calls = %#v, want explicit nil", body["parallel_tool_calls"])
				}
			},
		},
		{
			name:    "explicit empty include and contextManagement arrays are preserved",
			modelID: "gpt-4o",
			opts: provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{
						"include":           []interface{}{},
						"contextManagement": []interface{}{},
					},
				},
			},
			assertBody: func(t *testing.T, body map[string]interface{}) {
				includes, ok := body["include"].([]string)
				if !ok || len(includes) != 0 {
					t.Fatalf("include = %#v, want explicit empty []", body["include"])
				}
				cm, ok := body["context_management"].([]map[string]interface{})
				if !ok || len(cm) != 0 {
					t.Fatalf("context_management = %#v, want explicit empty []", body["context_management"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Config{APIKey: "test-key"})
			model := NewResponsesLanguageModel(p, tt.modelID)
			body, _, warnings, err := model.buildRequest(&tt.opts, tt.stream)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}
			if tt.assertBody != nil {
				tt.assertBody(t, body)
			}
			if tt.assertWarning != nil {
				tt.assertWarning(t, warnings)
			}
		})
	}
}

func TestResponsesLanguageModel_DefaultFileIDPrefixes(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				FileData: types.FileData{
					Type:       types.FileDataTypeData,
					DataString: "file-12345",
					MediaType:  "application/pdf",
				},
			}},
		}}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	parts := body["input"].([]interface{})[0].(responses.UserMessage).Content.([]interface{})
	file := parts[0].(map[string]interface{})
	if file["type"] != "input_file" || file["file_id"] != "file-12345" {
		t.Fatalf("file part = %#v, want TS-compatible file_id", file)
	}

	disabled := New(Config{APIKey: "test-key", FileIDPrefixes: []string{}})
	model = NewResponsesLanguageModel(disabled, "gpt-4o")
	body, _, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				FileData: types.FileData{
					Type:       types.FileDataTypeData,
					DataString: "file-12345",
					MediaType:  "application/pdf",
				},
			}},
		}}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody with disabled prefixes failed: %v", err)
	}
	parts = body["input"].([]interface{})[0].(responses.UserMessage).Content.([]interface{})
	file = parts[0].(map[string]interface{})
	if file["file_id"] != nil || !strings.HasPrefix(file["file_data"].(string), "data:application/pdf;base64,file-12345") {
		t.Fatalf("file part with disabled prefixes = %#v, want file_data", file)
	}
}

func assertHasWarning(t *testing.T, warnings []types.Warning, feature string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Type == "unsupported" && warning.Feature == feature {
			return
		}
	}
	t.Fatalf("warnings = %#v, missing unsupported %s", warnings, feature)
}

func assertNoWarning(t *testing.T, warnings []types.Warning, feature string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Type == "unsupported" && warning.Feature == feature {
			t.Fatalf("warnings = %#v, unexpectedly included unsupported %s", warnings, feature)
		}
	}
}

func ptrReasoning(value types.ReasoningLevel) *types.ReasoningLevel {
	return &value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestResponsesModel_Factory verifies the provider factory creates a valid model.
func TestResponsesModel_Factory(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model, err := p.ResponsesModel("gpt-4o")
	if err != nil {
		t.Fatalf("ResponsesModel failed: %v", err)
	}
	if model.ModelID() != "gpt-4o" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "gpt-4o")
	}
	if model.Provider() != "openai.responses" {
		t.Errorf("Provider() = %q, want %q", model.Provider(), "openai.responses")
	}
	if model.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
	}
}

// TestResponsesLanguageModel_CompactionTrigger covers row b6fff2e: the
// compactionTrigger option appends a compaction_trigger item to the end of
// the input array.
func TestResponsesLanguageModel_CompactionTrigger(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"compactionTrigger": true},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input := body["input"].([]interface{})
	last := input[len(input)-1].(map[string]interface{})
	if last["type"] != "compaction_trigger" {
		t.Fatalf("last input item = %#v, want compaction_trigger", last)
	}

	// Without the option, no compaction_trigger item is appended.
	body, _, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	for _, item := range body["input"].([]interface{}) {
		if m, ok := item.(map[string]interface{}); ok && m["type"] == "compaction_trigger" {
			t.Fatalf("unexpected compaction_trigger without the option: %#v", body["input"])
		}
	}
}

// TestResponsesLanguageModel_ReasoningEffortUpdate covers row 17e489e: a
// GPT-6+ model prepends a configuration_update item for reasoningEffortUpdate,
// and rejects it (with a warning) on older models or with auto truncation.
func TestResponsesLanguageModel_ReasoningEffortUpdate(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	gpt6 := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := gpt6.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffortUpdate": "high"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	input := body["input"].([]interface{})
	first := input[0].(map[string]interface{})
	if first["type"] != "configuration_update" {
		t.Fatalf("input[0] = %#v, want configuration_update first", first)
	}
	reasoning := first["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" {
		t.Fatalf("configuration_update reasoning = %#v, want effort high", reasoning)
	}

	// Older (non-GPT-6) models reject it with a warning; no item is prepended.
	older := NewResponsesLanguageModel(p, "gpt-5")
	body, _, warnings, err = older.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffortUpdate": "high"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 1 || warnings[0].Feature != "reasoningEffortUpdate" {
		t.Fatalf("warnings = %#v, want reasoningEffortUpdate warning", warnings)
	}
	for _, item := range body["input"].([]interface{}) {
		if m, ok := item.(map[string]interface{}); ok && m["type"] == "configuration_update" {
			t.Fatalf("unexpected configuration_update on non-GPT-6 model: %#v", body["input"])
		}
	}
}

// TestResponsesLanguageModel_GPT6DropsPromptCacheRetentionAndTopLogprobs
// covers rows 17e489e/b2b1bb9: GPT-6+ models don't support
// promptCacheRetention (use promptCacheOptions instead) or logprobs while
// reasoning, both dropped with a warning.
func TestResponsesLanguageModel_GPT6DropsPromptCacheRetentionAndTopLogprobs(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, ModelGPT6Astra)

	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"promptCacheRetention": "24h",
				"logprobs":             true,
				"reasoningEffort":      "high",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention should be dropped for GPT-6+: %#v", body)
	}
	if _, ok := body["top_logprobs"]; ok {
		t.Fatalf("top_logprobs should be dropped for GPT-6+ reasoning: %#v", body)
	}
	var sawRetentionWarning, sawLogprobsWarning bool
	for _, w := range warnings {
		if w.Feature == "promptCacheRetention" {
			sawRetentionWarning = true
		}
		if w.Feature == "logprobs" {
			sawLogprobsWarning = true
		}
	}
	if !sawRetentionWarning || !sawLogprobsWarning {
		t.Fatalf("warnings = %#v, want promptCacheRetention and logprobs warnings", warnings)
	}
}

// TestResponsesLanguageModel_ReasoningEffortValidatedForGPT6 covers row
// 17e489e: an unsupported reasoning effort for a GPT-6+ model is dropped
// with a warning instead of being sent.
func TestResponsesLanguageModel_ReasoningEffortValidatedForGPT6(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, ModelGPT6Astra)

	body, _, warnings, err := model.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "minimal"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("reasoning should be dropped for unsupported effort: %#v", body)
	}
	if len(warnings) != 1 || warnings[0].Feature != "reasoningEffort" {
		t.Fatalf("warnings = %#v, want reasoningEffort warning", warnings)
	}
}

// TestResponsesLanguageModel_ServiceTierFastGatedLikePriority covers row
// 4cd4548: serviceTier "fast" must be gated the same way as "priority"
// (previously it fell through to the default case and was always sent).
func TestResponsesLanguageModel_ServiceTierFastGatedLikePriority(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	supported := NewResponsesLanguageModel(p, "gpt-4o")
	body, _, err := supported.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"serviceTier": "fast"}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	if body["service_tier"] != "fast" {
		t.Fatalf("service_tier = %v, want fast for a supported model", body["service_tier"])
	}

	unsupported := NewResponsesLanguageModel(p, "gpt-5-nano")
	body, _, warnings, err := unsupported.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"serviceTier": "fast"}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("service_tier should be dropped for an unsupported model: %#v", body)
	}
	if len(warnings) != 1 || warnings[0].Feature != "serviceTier" {
		t.Fatalf("warnings = %#v, want serviceTier warning", warnings)
	}
}

// TestResponsesLanguageModel_PromptCacheOptionsPassthrough covers row
// b2b1bb9: promptCacheOptions is forwarded verbatim as prompt_cache_options.
func TestResponsesLanguageModel_PromptCacheOptionsPassthrough(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"promptCacheOptions": map[string]interface{}{"mode": "manual", "ttl": "24h"},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	opts, ok := body["prompt_cache_options"].(map[string]interface{})
	if !ok || opts["mode"] != "manual" || opts["ttl"] != "24h" {
		t.Fatalf("prompt_cache_options = %#v, want passthrough", body["prompt_cache_options"])
	}
}

// TestConvertResponsesUsage_CacheWriteTokens covers row b2b1bb9: usage's
// input_tokens_details.cache_write_tokens surfaces as
// InputDetails.CacheWriteTokens, and NoCacheTokens accounts for it.
func TestConvertResponsesUsage_CacheWriteTokens(t *testing.T) {
	cacheWrite := 5
	usage := responses.ResponsesAPIUsage{
		InputTokens:  100,
		OutputTokens: 20,
		InputTokensDetails: &struct {
			CachedTokens     int  `json:"cached_tokens,omitempty"`
			CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
		}{CachedTokens: 30, CacheWriteTokens: &cacheWrite},
	}
	got := convertResponsesUsage(&usage)
	if got.InputDetails == nil || got.InputDetails.CacheWriteTokens == nil || *got.InputDetails.CacheWriteTokens != 5 {
		t.Fatalf("InputDetails = %#v, want CacheWriteTokens=5", got.InputDetails)
	}
	if got.InputDetails.NoCacheTokens == nil || *got.InputDetails.NoCacheTokens != 65 {
		t.Fatalf("NoCacheTokens = %v, want 65 (100-30-5)", got.InputDetails.NoCacheTokens)
	}
}

// TestNormalizeResponsesToolSchemas covers d5e3024/411b3f2: function tool
// parameters (including namespaced tools) and the response_format schema are
// normalized for OpenAI structured outputs (propertyNames removed).
func TestNormalizeResponsesToolSchemas(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		Tools: []types.Tool{{
			Type: types.ToolTypeFunction,
			Name: "lookup",
			Parameters: map[string]interface{}{
				"type":          "object",
				"properties":    map[string]interface{}{"a": map[string]interface{}{"type": "string"}},
				"propertyNames": map[string]interface{}{"type": "string", "pattern": "^[a-z]+$"},
			},
		}},
		ResponseFormat: &provider.ResponseFormat{
			Type: "json",
			Schema: map[string]interface{}{
				"type":          "object",
				"propertyNames": map[string]interface{}{"type": "string", "pattern": "^[a-z]+$"},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	tools := body["tools"].([]interface{})
	fn := tools[0].(responses.FunctionToolDef)
	fnParams := fn.Parameters.(map[string]interface{})
	if _, ok := fnParams["propertyNames"]; ok {
		t.Fatalf("tool parameters propertyNames should be stripped: %#v", fnParams)
	}
	textObj := body["text"].(map[string]interface{})
	format := textObj["format"].(map[string]interface{})
	schema := format["schema"].(map[string]interface{})
	if _, ok := schema["propertyNames"]; ok {
		t.Fatalf("response_format schema propertyNames should be stripped: %#v", schema)
	}
}

// TestResponsesLanguageModel_DoGenerateNoOutputReturnsDescriptiveError covers
// row 75f86f4: a 200 response with no `output` field must raise a
// descriptive 500 ProviderError instead of silently producing an empty
// result.
func TestResponsesLanguageModel_DoGenerateNoOutputReturnsDescriptiveError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id": "resp_1", "status": "incomplete",
			"incomplete_details": map[string]interface{}{"reason": "max_output_tokens"},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err == nil {
		t.Fatal("expected an error for a response with no output")
	}
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) || provErr.StatusCode != 500 {
		t.Fatalf("error = %v, want a 500 ProviderError", err)
	}
	if !strings.Contains(err.Error(), "Responses API returned no output (max_output_tokens)") {
		t.Fatalf("error = %v, want descriptive no-output message", err)
	}
}

// TestResponsesLanguageModel_DoGenerateEmbeddedErrorMapsTo400 covers row
// 75f86f4: a 200 response with an embedded `error` object maps to a 400
// ProviderError.
func TestResponsesLanguageModel_DoGenerateEmbeddedErrorMapsTo400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"id":    "resp_1",
			"error": map[string]interface{}{"message": "bad prompt", "code": "invalid_request", "type": "invalid_request_error"},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err == nil {
		t.Fatal("expected an error for a response with an embedded error object")
	}
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) || provErr.StatusCode != 400 {
		t.Fatalf("error = %v, want a 400 ProviderError", err)
	}
	if !strings.Contains(err.Error(), "bad prompt") {
		t.Fatalf("error = %v, want to contain the embedded error message", err)
	}
}

// TestResponsesLanguageModel_NullUsageYieldsNilUsageFields covers row
// f6fac50: a JSON `null`/absent usage field yields an all-nil types.Usage
// instead of an all-zero usage that looks like a real (empty) response.
func TestResponsesLanguageModel_NullUsageYieldsNilUsageFields(t *testing.T) {
	got := convertResponsesUsage(nil)
	if got.InputTokens != nil || got.OutputTokens != nil || got.TotalTokens != nil {
		t.Fatalf("convertResponsesUsage(nil) = %#v, want all-nil fields", got)
	}
}

// TestResponsesLanguageModel_DoStreamChatCompletionsMismatchError covers row
// 1ead90c: a Chat Completions-shaped chunk (top-level "choices" array, no
// "type" discriminator) produces a helpful error instead of being silently
// skipped.
func TestResponsesLanguageModel_DoStreamChatCompletionsMismatchError(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"delta":{"content":"hi"}}]}

`)), false)
	defer stream.Close() //nolint:errcheck

	_, err := stream.Next()
	if err == nil {
		t.Fatal("expected an error for a Chat Completions-shaped chunk")
	}
	if !strings.Contains(err.Error(), "Received a Chat Completions stream while using the OpenAI Responses API") {
		t.Fatalf("error = %v, want the Chat Completions mismatch message", err)
	}
}

// TestResponsesLanguageModel_DoStreamKnownEventDecodeErrorForcesErrorFinish
// covers row eee6200: a decode failure on a known event type emits a
// ChunkTypeError chunk, and forces the eventual finish reason to "error"
// even though the terminal event itself reports a normal completion.
func TestResponsesLanguageModel_DoStreamKnownEventDecodeErrorForcesErrorFinish(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.delta","delta":123}

data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}

`)), false)
	defer stream.Close() //nolint:errcheck

	errChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if errChunk.Type != provider.ChunkTypeError {
		t.Fatalf("first chunk = %#v, want ChunkTypeError", errChunk)
	}

	finishChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if finishChunk.Type != provider.ChunkTypeFinish || finishChunk.FinishReason != types.FinishReasonError {
		t.Fatalf("finish chunk = %#v, want FinishReasonError forced by the earlier decode failure", finishChunk)
	}
}

// TestResponsesLanguageModel_ApplyPatchCallDecodesAsToolCall covers row
// 45f2b6a: apply_patch_call output items decode into a tool call
// ({callId,operation}) and drive a tool-calls finish reason, in both
// generate and stream.
func TestResponsesLanguageModel_ApplyPatchCallDecodesAsToolCall(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "apply_patch_call", "id": "ap_1", "call_id": "call_1", "status": "completed",
		"operation": map[string]interface{}{"type": "create_file", "path": "foo.go", "diff": "+package foo"},
	})
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		Output: []json.RawMessage{item},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, "")
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want one apply_patch tool call", result.ToolCalls)
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_1" || tc.ToolName != "openai.apply_patch" {
		t.Fatalf("tool call = %#v, want callId call_1 and toolName openai.apply_patch", tc)
	}
	if tc.Arguments["callId"] != "call_1" {
		t.Fatalf("arguments = %#v, want callId call_1", tc.Arguments)
	}
	operation, ok := tc.Arguments["operation"].(map[string]interface{})
	if !ok || operation["type"] != "create_file" || operation["path"] != "foo.go" {
		t.Fatalf("operation = %#v, want create_file foo.go", tc.Arguments["operation"])
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls", result.FinishReason)
	}
}

// TestResponsesLanguageModel_StreamApplyPatchCallDecodesAsToolCall covers row
// 45f2b6a for the streaming path.
func TestResponsesLanguageModel_StreamApplyPatchCallDecodesAsToolCall(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"apply_patch_call","id":"ap_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"apply_patch_call","id":"ap_1","call_id":"call_1","status":"completed","operation":{"type":"delete_file","path":"bar.go"}}}

`)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolCall || chunk.ToolCall == nil {
		t.Fatalf("chunk = %#v, want a tool call", chunk)
	}
	if chunk.ToolCall.ID != "call_1" || chunk.ToolCall.ToolName != "openai.apply_patch" {
		t.Fatalf("tool call = %#v, want callId call_1 toolName openai.apply_patch", chunk.ToolCall)
	}
	operation, ok := chunk.ToolCall.Arguments["operation"].(map[string]interface{})
	if !ok || operation["type"] != "delete_file" {
		t.Fatalf("operation = %#v, want delete_file", chunk.ToolCall.Arguments["operation"])
	}
}

// TestResponsesLanguageModel_RotatingItemIDUsesFirstSeenID covers row
// 73d48d0: reasoning-end uses the item id first seen at output_item.added
// for a given output_index, not a later (rotated) id from output_item.done.
func TestResponsesLanguageModel_RotatingItemIDUsesFirstSeenID(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_original"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_rotated","encrypted_content":"enc"}}

`)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeReasoningEnd || chunk.ID != "rs_original" {
		t.Fatalf("chunk = %#v, want reasoning-end with the original (first-seen) item id", chunk)
	}
}

// TestResponsesLanguageModel_StreamFailedRawFinishReason covers row
// e6376c2: a response.failed event carries a RawFinishReason on the finish
// chunk, using the incomplete reason if present or "error" otherwise.
func TestResponsesLanguageModel_StreamFailedRawFinishReason(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.created","response":{"id":"resp_1","created_at":1741269019,"model":"gpt-4o"}}

data: {"type":"response.failed","response":{"id":"resp_1","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":1}}}

`)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeFinish || chunk.RawFinishReason != "max_output_tokens" {
		t.Fatalf("finish chunk = %#v, want RawFinishReason=max_output_tokens", chunk)
	}
}

// TestResponsesLanguageModel_AsyncToolCallingGatedByModel covers row
// 4a09793: async=true on a function tool is sent for a GPT-6+ model, but
// dropped with a warning for an older model.
func TestResponsesLanguageModel_AsyncToolCallingGatedByModel(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	tools := []types.Tool{{
		Type: types.ToolTypeFunction,
		Name: "lookup",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"async": true},
		},
	}}

	gpt6 := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := gpt6.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		Tools:  tools,
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for a GPT-6+ model", warnings)
	}
	fn := body["tools"].([]interface{})[0].(responses.FunctionToolDef)
	if fn.Async == nil || !*fn.Async {
		t.Fatalf("Async = %v, want true for a GPT-6+ model", fn.Async)
	}

	older := NewResponsesLanguageModel(p, "gpt-4o")
	body, _, warnings, err = older.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		Tools:  tools,
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	fn = body["tools"].([]interface{})[0].(responses.FunctionToolDef)
	if fn.Async != nil {
		t.Fatalf("Async = %v, want nil (dropped) for an older model", fn.Async)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Feature, "async tool calling") {
		t.Fatalf("warnings = %#v, want an async tool calling warning", warnings)
	}
}

// TestResponsesLanguageModel_AsyncToolCallRoundTrip covers row 4a09793: a
// function_call output item with async=true decodes into
// ProviderMetadata.async, and replaying that tool call re-emits async on
// the function_call input item.
func TestResponsesLanguageModel_AsyncToolCallRoundTrip(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewResponsesLanguageModel(p, "gpt-4o")

	item, _ := json.Marshal(map[string]interface{}{
		"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "lookup",
		"arguments": "{}", "async": true,
	})
	result, err := model.convertResponse(responses.ResponsesAPIResponse{
		Output: []json.RawMessage{item},
		Usage:  &responses.ResponsesAPIUsage{InputTokens: 1, OutputTokens: 1},
	}, true, "")
	if err != nil {
		t.Fatalf("convertResponse failed: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want one", result.ToolCalls)
	}
	tc := result.ToolCalls[0]
	openaiMeta, ok := tc.ProviderMetadata["openai"].(map[string]interface{})
	if !ok || openaiMeta["async"] != true {
		t.Fatalf("ProviderMetadata = %#v, want async=true", tc.ProviderMetadata)
	}

	// Replay this tool call back into an input item.
	input, _, err := responses.ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{tc}}},
	}, "system", responses.ConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertPromptToInputWithOptions failed: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("input = %#v, want one function_call item", input)
	}
	fc, ok := input[0].(responses.FunctionCallItem)
	if !ok || fc.Async == nil || !*fc.Async {
		t.Fatalf("input[0] = %#v, want function_call with async=true", input[0])
	}
}

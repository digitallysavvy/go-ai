package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
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
		ID:    id,
		Model: "gpt-4o",
		Output: []json.RawMessage{content},
		Usage: responses.ResponsesAPIUsage{
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
		json.NewEncoder(w).Encode(mockResponsesResponse("resp_test", want))
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
		"id":        "call_abc",
		"call_id":   "call_abc",
		"name":      "get_weather",
		"arguments": `{"location":"NYC"}`,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses.ResponsesAPIResponse{
			ID:    "resp_tool",
			Model: "gpt-4o",
			Output: []json.RawMessage{callItem},
			Usage: responses.ResponsesAPIUsage{InputTokens: 5, OutputTokens: 5},
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
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("FinishReason = %q, want tool-calls", result.FinishReason)
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
			fmt.Fprintf(w, "data: %s\n\n", e)
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
	defer stream.Close()

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

// TestResponsesLanguageModel_DoStream_ToolCall verifies tool call accumulation in streaming.
func TestResponsesLanguageModel_DoStream_ToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		callDoneItem, _ := json.Marshal(map[string]interface{}{
			"type":      "function_call",
			"call_id":   "call_xyz",
			"name":      "search",
			"arguments": `{"query":"go lang"}`,
		})
		events := []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"call_xyz","call_id":"call_xyz","name":"search"}}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"query\":"}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"go lang\"}"}`,
			fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, string(callDoneItem)),
			`{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":5,"output_tokens":5}}}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
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
	defer stream.Close()

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
}

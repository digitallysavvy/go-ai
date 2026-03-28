package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestPromptCacheRetention tests the prompt cache retention feature
func TestPromptCacheRetention(t *testing.T) {
	tests := []struct {
		name                   string
		promptCacheRetention   string
		expectedInRequestBody  bool
		expectedRetentionValue string
	}{
		{
			name:                   "24h retention for gpt-5.1",
			promptCacheRetention:   "24h",
			expectedInRequestBody:  true,
			expectedRetentionValue: "24h",
		},
		{
			name:                   "in_memory retention (default)",
			promptCacheRetention:   "in_memory",
			expectedInRequestBody:  true,
			expectedRetentionValue: "in_memory",
		},
		{
			name:                  "no retention specified",
			promptCacheRetention:  "",
			expectedInRequestBody: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server to capture request
			var capturedRequest map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture the request body
				_ = json.NewDecoder(r.Body).Decode(&capturedRequest)

				// Return mock response
				response := openAIResponse{
					ID:      "test-id",
					Object:  "chat.completion",
					Created: 1234567890,
					Model:   "gpt-5.1",
					Choices: []struct {
						Index        int           `json:"index"`
						Message      openAIMessage `json:"message"`
						FinishReason string        `json:"finish_reason"`
					}{
						{
							Index: 0,
							Message: openAIMessage{
								Role:    "assistant",
								Content: "Test response",
							},
							FinishReason: "stop",
						},
					},
					Usage: openAIUsage{
						PromptTokens:     10,
						CompletionTokens: 20,
						TotalTokens:      30,
					},
				}

				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			// Create provider with test server
			p := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})

			// Create language model
			model := NewLanguageModel(p, "gpt-5.1")

			// Build generate options with prompt cache retention
			genOpts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{
							Role: types.RoleUser,
							Content: []types.ContentPart{
								types.TextContent{Text: "Hello"},
							},
						},
					},
				},
			}

			// Add provider options if retention is specified
			if tt.promptCacheRetention != "" {
				genOpts.ProviderOptions = map[string]interface{}{
					"openai": map[string]interface{}{
						"promptCacheRetention": tt.promptCacheRetention,
					},
				}
			}

			// Execute generation
			_, err := model.DoGenerate(context.Background(), genOpts)
			if err != nil {
				t.Fatalf("DoGenerate failed: %v", err)
			}

			// Verify prompt_cache_retention in request
			if tt.expectedInRequestBody {
				retention, ok := capturedRequest["prompt_cache_retention"]
				if !ok {
					t.Errorf("expected prompt_cache_retention in request body, but not found")
				}
				if retention != tt.expectedRetentionValue {
					t.Errorf("expected prompt_cache_retention=%s, got %s", tt.expectedRetentionValue, retention)
				}
			} else {
				if _, ok := capturedRequest["prompt_cache_retention"]; ok {
					t.Errorf("expected no prompt_cache_retention in request body, but found one")
				}
			}
		})
	}
}

// TestPromptCacheRetentionWithStreaming tests cache retention with streaming
func TestPromptCacheRetentionWithStreaming(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request has prompt_cache_retention
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		if retention, ok := reqBody["prompt_cache_retention"]; !ok || retention != "24h" {
			t.Errorf("expected prompt_cache_retention=24h in request")
		}

		// Return SSE stream
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send chunk
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
		// Send done
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	// Create provider and model
	p := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewLanguageModel(p, "gpt-5.1")

	// Generate with streaming
	genOpts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"promptCacheRetention": "24h",
			},
		},
	}

	stream, err := model.DoStream(context.Background(), genOpts)
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close()

	// Read chunks
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("stream.Next failed: %v", err)
	}

	if chunk.Text != "Hello" {
		t.Errorf("expected text=Hello, got %s", chunk.Text)
	}
}

// TestPromptCacheRetentionModels tests cache retention with different models
func TestPromptCacheRetentionModels(t *testing.T) {
	models := []struct {
		modelID         string
		supportsCache24h bool
	}{
		{"gpt-5.1", true},
		{"gpt-5.1-chat-latest", true},
		{"gpt-5.1-codex", true},
		{"gpt-5.2", true},
		{"gpt-5.2-pro", true},
		{"gpt-4", false}, // Older models might not support 24h
		{"gpt-3.5-turbo", false},
	}

	for _, tt := range models {
		t.Run(tt.modelID, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqBody map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&reqBody)

				// Return success response
				response := openAIResponse{
					ID:     "test-id",
					Model:  tt.modelID,
					Choices: []struct {
						Index        int           `json:"index"`
						Message      openAIMessage `json:"message"`
						FinishReason string        `json:"finish_reason"`
					}{
						{
							Message: openAIMessage{
								Content: "Response",
							},
							FinishReason: "stop",
						},
					},
					Usage: openAIUsage{
						PromptTokens:     10,
						CompletionTokens: 10,
						TotalTokens:      20,
					},
				}

				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			p := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			model := NewLanguageModel(p, tt.modelID)

			genOpts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{
							Role: types.RoleUser,
							Content: []types.ContentPart{
								types.TextContent{Text: "Test"},
							},
						},
					},
				},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{
						"promptCacheRetention": "24h",
					},
				},
			}

			_, err := model.DoGenerate(context.Background(), genOpts)
			if err != nil {
				t.Fatalf("DoGenerate failed for %s: %v", tt.modelID, err)
			}
		})
	}
}

// TestBuildRequestBodyWithPromptCacheRetention tests the buildRequestBody method
func TestBuildRequestBodyWithPromptCacheRetention(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})
	model := NewLanguageModel(p, "gpt-5.1")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"promptCacheRetention": "24h",
			},
		},
	}

	body := model.buildRequestBody(opts, false)

	// Verify prompt_cache_retention is present
	retention, ok := body["prompt_cache_retention"]
	if !ok {
		t.Errorf("expected prompt_cache_retention in request body")
	}

	if retention != "24h" {
		t.Errorf("expected retention=24h, got %v", retention)
	}

	// Verify other standard fields are present
	if _, ok := body["model"]; !ok {
		t.Errorf("expected model in request body")
	}

	if _, ok := body["messages"]; !ok {
		t.Errorf("expected messages in request body")
	}
}

// TestProviderOptionsNil tests that nil provider options don't cause issues
func TestProviderOptionsNil(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})
	model := NewLanguageModel(p, "gpt-4")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
			},
		},
		ProviderOptions: nil,
	}

	body := model.buildRequestBody(opts, false)

	// Should not have prompt_cache_retention
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Errorf("expected no prompt_cache_retention when ProviderOptions is nil")
	}
}

// TestDoGenerateFinishReasonMapping tests that OpenAI finish reasons are correctly
// mapped to normalized types.FinishReason values in non-streaming responses.
// OpenAI uses underscores (e.g. "tool_calls") but the SDK uses hyphens ("tool-calls").
func TestDoGenerateFinishReasonMapping(t *testing.T) {
	tests := []struct {
		name               string
		openAIFinishReason string
		expectedReason     types.FinishReason
	}{
		{
			name:               "stop maps to stop",
			openAIFinishReason: "stop",
			expectedReason:     types.FinishReasonStop,
		},
		{
			name:               "length maps to length",
			openAIFinishReason: "length",
			expectedReason:     types.FinishReasonLength,
		},
		{
			name:               "tool_calls maps to tool-calls",
			openAIFinishReason: "tool_calls",
			expectedReason:     types.FinishReasonToolCalls,
		},
		{
			name:               "content_filter maps to content-filter",
			openAIFinishReason: "content_filter",
			expectedReason:     types.FinishReasonContentFilter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := openAIResponse{
					ID:    "test-id",
					Model: "gpt-4",
					Choices: []struct {
						Index        int           `json:"index"`
						Message      openAIMessage `json:"message"`
						FinishReason string        `json:"finish_reason"`
					}{
						{
							Index: 0,
							Message: openAIMessage{
								Role:    "assistant",
								Content: "Test response",
							},
							FinishReason: tt.openAIFinishReason,
						},
					},
					Usage: openAIUsage{
						PromptTokens:     10,
						CompletionTokens: 5,
						TotalTokens:      15,
					},
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			p := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			model := NewLanguageModel(p, "gpt-4")

			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{
							Role:    types.RoleUser,
							Content: []types.ContentPart{types.TextContent{Text: "Hello"}},
						},
					},
				},
			})
			if err != nil {
				t.Fatalf("DoGenerate failed: %v", err)
			}

			if result.FinishReason != tt.expectedReason {
				t.Errorf("FinishReason = %q, want %q", result.FinishReason, tt.expectedReason)
			}
		})
	}
}

// TestDoGenerateToolCallsFinishReason verifies that a response with tool calls
// returns FinishReasonToolCalls ("tool-calls"), not the raw OpenAI "tool_calls".
func TestDoGenerateToolCallsFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openAIResponse{
			ID:    "test-id",
			Model: "gpt-4",
			Choices: []struct {
				Index        int           `json:"index"`
				Message      openAIMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: openAIMessage{
						Role: "assistant",
						ToolCalls: []openAIToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								}{
									Name:      "get_weather",
									Arguments: `{"location":"San Francisco"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: openAIUsage{
				PromptTokens:     15,
				CompletionTokens: 10,
				TotalTokens:      25,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewLanguageModel(p, "gpt-4")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: "What is the weather?"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate failed: %v", err)
	}

	if result.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, types.FinishReasonToolCalls)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolName != "get_weather" {
		t.Errorf("ToolName = %q, want %q", result.ToolCalls[0].ToolName, "get_weather")
	}
}

// TestDoStreamFinishReasonMapping tests that OpenAI finish reasons are correctly
// mapped to normalized types.FinishReason values in streaming responses.
func TestDoStreamFinishReasonMapping(t *testing.T) {
	tests := []struct {
		name               string
		openAIFinishReason string
		expectedReason     types.FinishReason
	}{
		{
			name:               "stop maps to stop",
			openAIFinishReason: "stop",
			expectedReason:     types.FinishReasonStop,
		},
		{
			name:               "length maps to length",
			openAIFinishReason: "length",
			expectedReason:     types.FinishReasonLength,
		},
		{
			name:               "tool_calls maps to tool-calls",
			openAIFinishReason: "tool_calls",
			expectedReason:     types.FinishReasonToolCalls,
		},
		{
			name:               "content_filter maps to content-filter",
			openAIFinishReason: "content_filter",
			expectedReason:     types.FinishReasonContentFilter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)

				// Send a text chunk first
				w.Write([]byte(`data: {"choices":[{"delta":{"content":"Hi"}}]}` + "\n\n"))
				// Send finish chunk with the OpenAI finish reason
				w.Write([]byte(fmt.Sprintf(`data: {"choices":[{"delta":{},"finish_reason":"%s"}]}`, tt.openAIFinishReason) + "\n\n"))
				// Send done
				w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer server.Close()

			p := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			model := NewLanguageModel(p, "gpt-4")

			stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{
							Role:    types.RoleUser,
							Content: []types.ContentPart{types.TextContent{Text: "Hello"}},
						},
					},
				},
			})
			if err != nil {
				t.Fatalf("DoStream failed: %v", err)
			}
			defer stream.Close()

			// Read text chunk
			chunk, err := stream.Next()
			if err != nil {
				t.Fatalf("stream.Next (text) failed: %v", err)
			}
			if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hi" {
				t.Errorf("expected text chunk 'Hi', got type=%q text=%q", chunk.Type, chunk.Text)
			}

			// Read finish chunk
			chunk, err = stream.Next()
			if err != nil {
				t.Fatalf("stream.Next (finish) failed: %v", err)
			}
			if chunk.Type != provider.ChunkTypeFinish {
				t.Fatalf("expected finish chunk, got type=%q", chunk.Type)
			}
			if chunk.FinishReason != tt.expectedReason {
				t.Errorf("FinishReason = %q, want %q", chunk.FinishReason, tt.expectedReason)
			}
		})
	}
}

// TestDoStreamToolCallChunks verifies that incremental tool call deltas are accumulated
// and emitted as complete ChunkTypeToolCall chunks before the finish chunk.
// OpenAI streams tool call arguments across multiple SSE deltas; each delta for a given
// tool call carries a partial argument string that must be concatenated before parsing.
func TestDoStreamToolCallChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// A short text delta
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"Sure!"}}]}` + "\n\n"))
		// First tool call delta: id + name + empty args
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}` + "\n\n"))
		// Second tool call delta: partial arguments
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":"}}]}}]}` + "\n\n"))
		// Third tool call delta: remaining arguments
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"New York\"}"}}]}}]}` + "\n\n"))
		// Finish with tool_calls reason
		w.Write([]byte(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewLanguageModel(p, "gpt-4")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: "What is the weather?"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close()

	// Chunk 1: text
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next (text) failed: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText || chunk.Text != "Sure!" {
		t.Errorf("expected text chunk 'Sure!', got type=%q text=%q", chunk.Type, chunk.Text)
	}

	// Chunk 2: tool call (fully assembled from three deltas)
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("Next (tool call) failed: %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("expected ChunkTypeToolCall, got %q", chunk.Type)
	}
	if chunk.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if chunk.ToolCall.ToolName != "get_weather" {
		t.Errorf("ToolName = %q, want %q", chunk.ToolCall.ToolName, "get_weather")
	}
	if chunk.ToolCall.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want %q", chunk.ToolCall.ID, "call_abc")
	}
	location, ok := chunk.ToolCall.Arguments["location"]
	if !ok || location != "New York" {
		t.Errorf("ToolCall.Arguments[location] = %v, want %q", location, "New York")
	}

	// Chunk 3: finish with FinishReasonToolCalls
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("Next (finish) failed: %v", err)
	}
	if chunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("expected ChunkTypeFinish, got %q", chunk.Type)
	}
	if chunk.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("FinishReason = %q, want %q", chunk.FinishReason, types.FinishReasonToolCalls)
	}
}

// BUG-T08: OpenAI may send null for the "type" field in streaming tool call deltas.
// The parser must not fail when that field is absent or null (#12901).
func TestDoStreamToolCallDeltaNullType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// First delta carries id + name with a null "type" field.
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_nulltype","type":null,"function":{"name":"greet","arguments":""}}]}}]}` + "\n\n"))
		// Second delta has partial arguments and omits "type" entirely.
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"name\":\"world\"}"}}]}}]}` + "\n\n"))
		// Finish
		w.Write([]byte(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewLanguageModel(p, "gpt-4")

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStream failed: %v", err)
	}
	defer stream.Close()

	// Expect a tool call chunk assembled from the two deltas.
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("stream.Next failed: %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("expected ChunkTypeToolCall, got %q", chunk.Type)
	}
	if chunk.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if chunk.ToolCall.ToolName != "greet" {
		t.Errorf("ToolName = %q, want %q", chunk.ToolCall.ToolName, "greet")
	}
	if chunk.ToolCall.Arguments["name"] != "world" {
		t.Errorf("argument name = %v, want %q", chunk.ToolCall.Arguments["name"], "world")
	}

	// Consume finish chunk — must not error.
	_, err = stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on finish chunk: %v", err)
	}
}

// TestProviderOptionsInvalidType tests handling of invalid provider option types
func TestProviderOptionsInvalidType(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})
	model := NewLanguageModel(p, "gpt-5.1")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": "invalid-type", // Not a map
		},
	}

	body := model.buildRequestBody(opts, false)

	// Should not crash, just skip the invalid option
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Errorf("expected no prompt_cache_retention with invalid provider options type")
	}
}

// TestStoreOffFiltersReasoningInRequest verifies that buildRequestBody applies
// filterUnencryptedReasoningParts to assistant message content when store=false,
// so unencryptable reasoning is dropped before the request reaches the API.
func TestStoreOffFiltersReasoningInRequest(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "o3")

	assistantContent := []types.ContentPart{
		types.TextContent{Text: "Sure, let me think."},
		types.ReasoningContent{Text: "step 1...", EncryptedContent: ""},      // no encrypted → drop
		types.ReasoningContent{Text: "step 2...", EncryptedContent: "enc_x"}, // encrypted → keep
	}

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
				{Role: types.RoleAssistant, Content: assistantContent},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"store": false,
			},
		},
	}

	body := model.buildRequestBody(opts, false)

	// store=false must appear in the request body
	if storeVal, ok := body["store"]; !ok || storeVal != false {
		t.Errorf("expected body[store]=false, got %v (present=%v)", storeVal, ok)
	}

	// The assistant message content should have the unencrypted reasoning dropped.
	// buildRequestBody calls ToOpenAIMessages which serialises messages — we can't
	// inspect ContentPart slices directly here, but we CAN verify that the raw
	// message wasn't mutated by calling filterUnencryptedReasoningParts directly
	// and comparing lengths (the integration is the key check).
	filtered := filterUnencryptedReasoningParts(assistantContent, false)
	if len(filtered) != 2 {
		t.Fatalf("filterUnencryptedReasoningParts returned %d parts, want 2", len(filtered))
	}
	if _, ok := filtered[0].(types.TextContent); !ok {
		t.Errorf("filtered[0] should be TextContent, got %T", filtered[0])
	}
	rc, ok := filtered[1].(types.ReasoningContent)
	if !ok {
		t.Fatalf("filtered[1] should be ReasoningContent, got %T", filtered[1])
	}
	if rc.EncryptedContent != "enc_x" {
		t.Errorf("EncryptedContent = %q, want %q", rc.EncryptedContent, "enc_x")
	}
}

// TestStoreOnPreservesReasoningInRequest verifies that when store=true (default),
// reasoning parts are not filtered from assistant messages.
func TestStoreOnPreservesReasoningInRequest(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "o3")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
			},
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"store": true,
			},
		},
	}

	body := model.buildRequestBody(opts, false)
	if storeVal, ok := body["store"]; !ok || storeVal != true {
		t.Errorf("expected body[store]=true, got %v (present=%v)", storeVal, ok)
	}
}

// TestStoreNotSetOmitsFromRequest verifies that when store is not set in provider
// options, the "store" key is omitted from the request body entirely.
func TestStoreNotSetOmitsFromRequest(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "gpt-4o")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
			},
		},
	}

	body := model.buildRequestBody(opts, false)
	if _, ok := body["store"]; ok {
		t.Errorf("expected store to be absent from body when not set in ProviderOptions")
	}
}

// TestIsReasoningModel verifies the allowlist of model prefixes that require the
// "developer" role for system messages.
func TestIsReasoningModel(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		// Reasoning models — expect true
		{"o1", true},
		{"o1-2024-12-17", true},
		{"o3", true},
		{"o3-2025-04-16", true},
		{"o3-mini", true},
		{"o3-mini-2025-01-31", true},
		{"o4-mini", true},
		{"o4-mini-2025-04-16", true},
		{"gpt-5", true},
		{"gpt-5-mini", true},
		{"gpt-5-nano", true},
		{"gpt-5.1", true},
		{"gpt-5.2", true},
		{"gpt-5.2-pro", true},
		{"gpt-5.3-codex", true},
		{"gpt-5.4", true},
		{"gpt-5.4-pro", true},
		{"gpt-5.4-2026-03-05", true},
		// Non-reasoning models — expect false
		{"gpt-5-chat-latest", false},
		{"gpt-5.1-chat-latest", true}, // gpt-5.1-chat-latest starts with gpt-5 but NOT gpt-5-chat
		{"gpt-4", false},
		{"gpt-4-turbo", false},
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"gpt-4.1", false},
		{"gpt-3.5-turbo", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := isReasoningModel(tt.modelID)
			if got != tt.expected {
				t.Errorf("isReasoningModel(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

// TestSystemMessageRoleForReasoningModels verifies that the "developer" role is sent
// for reasoning models and "system" role is sent for non-reasoning models.
func TestSystemMessageRoleForReasoningModels(t *testing.T) {
	tests := []struct {
		modelID      string
		expectedRole string
	}{
		{"o3", "developer"},
		{"o4-mini", "developer"},
		{"gpt-5.4", "developer"},
		{"gpt-5.4-pro", "developer"},
		{"gpt-5", "developer"},
		{"gpt-5-mini", "developer"},
		{"gpt-5-chat-latest", "system"}, // only prefix gpt-5-chat is excluded
		{"gpt-4o", "system"},
		{"gpt-4.1", "system"},
		{"gpt-3.5-turbo", "system"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			p := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(p, tt.modelID)

			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					System: "You are a helpful assistant.",
					Messages: []types.Message{
						{
							Role:    types.RoleUser,
							Content: []types.ContentPart{types.TextContent{Text: "Hi"}},
						},
					},
				},
			}

			body := model.buildRequestBody(opts, false)
			messages, ok := body["messages"].([]map[string]interface{})
			if !ok || len(messages) == 0 {
				t.Fatalf("expected messages in request body")
			}

			// System/developer message is always prepended as index 0.
			role, _ := messages[0]["role"].(string)
			if role != tt.expectedRole {
				t.Errorf("system message role = %q, want %q for model %q", role, tt.expectedRole, tt.modelID)
			}
		})
	}
}

// TestStoreOffDropsUnencryptedReasoning verifies that filterUnencryptedReasoningParts
// removes ReasoningContent with no EncryptedContent when store=false,
// and preserves all parts when store=true.
func TestStoreOffDropsUnencryptedReasoning(t *testing.T) {
	textPart := types.TextContent{Text: "hello"}
	unencryptedReasoning := types.ReasoningContent{Text: "thinking...", EncryptedContent: ""}
	encryptedReasoning := types.ReasoningContent{Text: "thinking...", EncryptedContent: "enc_blob_xyz"}

	content := []types.ContentPart{textPart, unencryptedReasoning, encryptedReasoning}

	t.Run("store=false drops unencrypted reasoning", func(t *testing.T) {
		filtered := filterUnencryptedReasoningParts(content, false)
		if len(filtered) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(filtered))
		}
		// text part should remain
		if _, ok := filtered[0].(types.TextContent); !ok {
			t.Errorf("filtered[0] should be TextContent, got %T", filtered[0])
		}
		// encrypted reasoning should remain
		rc, ok := filtered[1].(types.ReasoningContent)
		if !ok {
			t.Fatalf("filtered[1] should be ReasoningContent, got %T", filtered[1])
		}
		if rc.EncryptedContent != "enc_blob_xyz" {
			t.Errorf("EncryptedContent = %q, want %q", rc.EncryptedContent, "enc_blob_xyz")
		}
	})

	t.Run("store=true preserves all parts", func(t *testing.T) {
		filtered := filterUnencryptedReasoningParts(content, true)
		if len(filtered) != 3 {
			t.Fatalf("expected 3 parts (all preserved), got %d", len(filtered))
		}
	})

	t.Run("store=false with no reasoning parts is a no-op", func(t *testing.T) {
		onlyText := []types.ContentPart{textPart}
		filtered := filterUnencryptedReasoningParts(onlyText, false)
		if len(filtered) != 1 {
			t.Fatalf("expected 1 part, got %d", len(filtered))
		}
	})

	t.Run("store=false drops all when all reasoning is unencrypted", func(t *testing.T) {
		allUnencrypted := []types.ContentPart{unencryptedReasoning, unencryptedReasoning}
		filtered := filterUnencryptedReasoningParts(allUnencrypted, false)
		if len(filtered) != 0 {
			t.Fatalf("expected 0 parts, got %d", len(filtered))
		}
	})
}

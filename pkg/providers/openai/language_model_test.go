package openai

import (
	"context"
	"encoding/json"
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
				json.NewDecoder(r.Body).Decode(&capturedRequest)

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

				json.NewEncoder(w).Encode(response)
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
		json.NewDecoder(r.Body).Decode(&reqBody)

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
				json.NewDecoder(r.Body).Decode(&reqBody)

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

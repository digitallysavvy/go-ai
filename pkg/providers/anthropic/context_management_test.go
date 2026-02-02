package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextManagementSerialization tests that ContextManagement serializes correctly
func TestContextManagementSerialization(t *testing.T) {
	tests := []struct {
		name     string
		cm       ContextManagement
		expected string
	}{
		{
			name: "single strategy - clear_tool_uses",
			cm: ContextManagement{
				Strategies: []string{StrategyClearToolUses},
			},
			expected: `{"strategies":["clear_tool_uses"]}`,
		},
		{
			name: "single strategy - clear_thinking",
			cm: ContextManagement{
				Strategies: []string{StrategyClearThinking},
			},
			expected: `{"strategies":["clear_thinking"]}`,
		},
		{
			name: "multiple strategies",
			cm: ContextManagement{
				Strategies: []string{StrategyClearToolUses, StrategyClearThinking},
			},
			expected: `{"strategies":["clear_tool_uses","clear_thinking"]}`,
		},
		{
			name:     "empty strategies",
			cm:       ContextManagement{},
			expected: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.cm)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

// TestContextManagementResponseDeserialization tests parsing of ContextManagementResponse
func TestContextManagementResponseDeserialization(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		expected ContextManagementResponse
	}{
		{
			name: "tool uses cleared",
			jsonData: `{
				"messages_deleted": 2,
				"tool_uses_cleared": 5
			}`,
			expected: ContextManagementResponse{
				MessagesDeleted: 2,
				ToolUsesCleared: 5,
			},
		},
		{
			name: "thinking blocks cleared",
			jsonData: `{
				"messages_truncated": 1,
				"thinking_blocks_cleared": 3
			}`,
			expected: ContextManagementResponse{
				MessagesTruncated:     1,
				ThinkingBlocksCleared: 3,
			},
		},
		{
			name: "all fields populated",
			jsonData: `{
				"messages_deleted": 1,
				"messages_truncated": 2,
				"tool_uses_cleared": 3,
				"thinking_blocks_cleared": 4
			}`,
			expected: ContextManagementResponse{
				MessagesDeleted:       1,
				MessagesTruncated:     2,
				ToolUsesCleared:       3,
				ThinkingBlocksCleared: 4,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result ContextManagementResponse
			err := json.Unmarshal([]byte(tt.jsonData), &result)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseContextManagement_RootLevel tests parsing context management from root level
func TestParseContextManagement_RootLevel(t *testing.T) {
	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello"}],
		"model": "claude-sonnet-4",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		},
		"context_management": {
			"messages_deleted": 2,
			"tool_uses_cleared": 5
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)
	require.NotNil(t, response.ContextManagement)
	assert.Equal(t, 2, response.ContextManagement.MessagesDeleted)
	assert.Equal(t, 5, response.ContextManagement.ToolUsesCleared)
}

// TestParseContextManagement_UsageLevel tests parsing context management from usage block (legacy)
func TestParseContextManagement_UsageLevel(t *testing.T) {
	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello"}],
		"model": "claude-sonnet-4",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"context_management": {
				"thinking_blocks_cleared": 3
			}
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)
	require.NotNil(t, response.Usage.ContextManagement)
	assert.Equal(t, 3, response.Usage.ContextManagement.ThinkingBlocksCleared)
}

// TestParseContextManagement_RootLevelTakesPrecedence tests that root level is preferred
func TestParseContextManagement_RootLevelTakesPrecedence(t *testing.T) {
	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello"}],
		"model": "claude-sonnet-4",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"context_management": {
				"messages_deleted": 1
			}
		},
		"context_management": {
			"messages_deleted": 2,
			"tool_uses_cleared": 5
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)

	// Both should be parsed
	require.NotNil(t, response.ContextManagement)
	require.NotNil(t, response.Usage.ContextManagement)

	// Root level should have correct values
	assert.Equal(t, 2, response.ContextManagement.MessagesDeleted)
	assert.Equal(t, 5, response.ContextManagement.ToolUsesCleared)

	// Usage level should also have its values
	assert.Equal(t, 1, response.Usage.ContextManagement.MessagesDeleted)
}

// TestModelOptionsWithContextManagement tests creating a model with context management options
func TestModelOptionsWithContextManagement(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})

	options := &ModelOptions{
		ContextManagement: &ContextManagement{
			Strategies: []string{StrategyClearToolUses},
		},
	}

	model, err := p.LanguageModelWithOptions("claude-sonnet-4", options)
	require.NoError(t, err)
	require.NotNil(t, model)

	lm, ok := model.(*LanguageModel)
	require.True(t, ok)
	require.NotNil(t, lm.options)
	require.NotNil(t, lm.options.ContextManagement)
	assert.Equal(t, []string{StrategyClearToolUses}, lm.options.ContextManagement.Strategies)
}

// TestDoGenerate_WithContextManagement tests non-streaming generation with context management
func TestDoGenerate_WithContextManagement(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify beta header is present
		betaHeader := r.Header.Get("anthropic-beta")
		assert.Equal(t, BetaHeaderContextManagement, betaHeader)

		// Parse request body
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify context_management is in request
		_, ok := reqBody["context_management"]
		require.True(t, ok)

		// Return mock response with context management
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]string{{"type": "text", "text": "Hello"}},
			"model":       "claude-sonnet-4",
			"stop_reason": "end_turn",
			"usage": map[string]int{
				"input_tokens":  100,
				"output_tokens": 50,
			},
			"context_management": map[string]int{
				"messages_deleted":  2,
				"tool_uses_cleared": 5,
			},
		})
	}))
	defer server.Close()

	// Create provider with test server
	p := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	// Create model with context management
	model, err := p.LanguageModelWithOptions("claude-sonnet-4", &ModelOptions{
		ContextManagement: &ContextManagement{
			Strategies: []string{StrategyClearToolUses},
		},
	})
	require.NoError(t, err)

	// Generate text
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Hello",
		},
		MaxTokens: intPtr(100),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify context management was extracted
	require.NotNil(t, result.ContextManagement)
	cm, ok := result.ContextManagement.(*ContextManagementResponse)
	require.True(t, ok)
	assert.Equal(t, 2, cm.MessagesDeleted)
	assert.Equal(t, 5, cm.ToolUsesCleared)
}

// TestDoStream_WithContextManagement tests streaming with context management
func TestDoStream_WithContextManagement(t *testing.T) {
	// Create mock server that returns SSE stream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify beta header is present
		betaHeader := r.Header.Get("anthropic-beta")
		assert.Equal(t, BetaHeaderContextManagement, betaHeader)

		// Parse request body
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify context_management is in request
		_, ok := reqBody["context_management"]
		require.True(t, ok)

		// Return SSE stream
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// Send events
		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant"}}

`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50},"context_management":{"messages_deleted":2,"tool_uses_cleared":5}}

`,
			`event: message_stop
data: {"type":"message_stop"}

`,
		}

		for _, event := range events {
			w.Write([]byte(event))
			flusher.Flush()
		}
	}))
	defer server.Close()

	// Create provider with test server
	p := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	// Create model with context management
	model, err := p.LanguageModelWithOptions("claude-sonnet-4", &ModelOptions{
		ContextManagement: &ContextManagement{
			Strategies: []string{StrategyClearToolUses},
		},
	})
	require.NoError(t, err)

	// Stream text
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Hello",
		},
		MaxTokens: intPtr(100),
	})
	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Close()

	// Read chunks
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, chunk)
	}

	// Verify we got text and finish chunks
	require.True(t, len(chunks) >= 2)

	// Find text chunk
	var textChunk *provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkTypeText {
			textChunk = chunk
			break
		}
	}
	require.NotNil(t, textChunk)
	assert.Equal(t, "Hello", textChunk.Text)

	// Find finish chunk
	var finishChunk *provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkTypeFinish {
			finishChunk = chunk
			break
		}
	}
	require.NotNil(t, finishChunk)
	assert.Equal(t, types.FinishReasonStop, finishChunk.FinishReason)

	// Verify context management was extracted
	require.NotNil(t, finishChunk.ContextManagement)
	cm, ok := finishChunk.ContextManagement.(*ContextManagementResponse)
	require.True(t, ok)
	assert.Equal(t, 2, cm.MessagesDeleted)
	assert.Equal(t, 5, cm.ToolUsesCleared)
}

// TestBuildRequestBody_WithContextManagement tests that context management is added to request
func TestBuildRequestBody_WithContextManagement(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})

	model, err := p.LanguageModelWithOptions("claude-sonnet-4", &ModelOptions{
		ContextManagement: &ContextManagement{
			Strategies: []string{StrategyClearToolUses, StrategyClearThinking},
		},
	})
	require.NoError(t, err)

	lm := model.(*LanguageModel)

	body := lm.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Hello",
		},
		MaxTokens: intPtr(100),
	}, false)

	// Verify context_management is in request body
	contextManagement, ok := body["context_management"]
	require.True(t, ok)

	// Serialize to JSON and verify structure
	data, err := json.Marshal(contextManagement)
	require.NoError(t, err)

	var cm ContextManagement
	err = json.Unmarshal(data, &cm)
	require.NoError(t, err)
	assert.Len(t, cm.Strategies, 2)
	assert.Contains(t, cm.Strategies, StrategyClearToolUses)
	assert.Contains(t, cm.Strategies, StrategyClearThinking)
}

// TestBuildRequestBody_WithoutContextManagement tests that context management is not added when not configured
func TestBuildRequestBody_WithoutContextManagement(t *testing.T) {
	p := New(Config{
		APIKey: "test-key",
	})

	model, err := p.LanguageModel("claude-sonnet-4")
	require.NoError(t, err)

	lm := model.(*LanguageModel)

	body := lm.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Hello",
		},
		MaxTokens: intPtr(100),
	}, false)

	// Verify context_management is NOT in request body
	_, ok := body["context_management"]
	assert.False(t, ok)
}

// TestConvertResponse_ExtractsContextManagement tests that convertResponse extracts context management
func TestConvertResponse_ExtractsContextManagement(t *testing.T) {
	tests := []struct {
		name     string
		response anthropicResponse
		expected *ContextManagementResponse
	}{
		{
			name: "root level context management",
			response: anthropicResponse{
				ID:         "msg_123",
				Type:       "message",
				Role:       "assistant",
				Content:    []anthropicContent{{Type: "text", Text: "Hello"}},
				StopReason: "end_turn",
				Usage: anthropicUsage{
					InputTokens:  100,
					OutputTokens: 50,
				},
				ContextManagement: &ContextManagementResponse{
					MessagesDeleted: 2,
					ToolUsesCleared: 5,
				},
			},
			expected: &ContextManagementResponse{
				MessagesDeleted: 2,
				ToolUsesCleared: 5,
			},
		},
		{
			name: "usage level context management (legacy)",
			response: anthropicResponse{
				ID:         "msg_123",
				Type:       "message",
				Role:       "assistant",
				Content:    []anthropicContent{{Type: "text", Text: "Hello"}},
				StopReason: "end_turn",
				Usage: anthropicUsage{
					InputTokens:  100,
					OutputTokens: 50,
					ContextManagement: &ContextManagementResponse{
						ThinkingBlocksCleared: 3,
					},
				},
			},
			expected: &ContextManagementResponse{
				ThinkingBlocksCleared: 3,
			},
		},
		{
			name: "root level takes precedence",
			response: anthropicResponse{
				ID:         "msg_123",
				Type:       "message",
				Role:       "assistant",
				Content:    []anthropicContent{{Type: "text", Text: "Hello"}},
				StopReason: "end_turn",
				Usage: anthropicUsage{
					InputTokens:  100,
					OutputTokens: 50,
					ContextManagement: &ContextManagementResponse{
						MessagesDeleted: 1,
					},
				},
				ContextManagement: &ContextManagementResponse{
					MessagesDeleted: 2,
					ToolUsesCleared: 5,
				},
			},
			expected: &ContextManagementResponse{
				MessagesDeleted: 2,
				ToolUsesCleared: 5,
			},
		},
		{
			name: "no context management",
			response: anthropicResponse{
				ID:         "msg_123",
				Type:       "message",
				Role:       "assistant",
				Content:    []anthropicContent{{Type: "text", Text: "Hello"}},
				StopReason: "end_turn",
				Usage: anthropicUsage{
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &LanguageModel{}
			result := model.convertResponse(tt.response)

			if tt.expected == nil {
				assert.Nil(t, result.ContextManagement)
			} else {
				require.NotNil(t, result.ContextManagement)
				cm, ok := result.ContextManagement.(*ContextManagementResponse)
				require.True(t, ok)
				assert.Equal(t, tt.expected.MessagesDeleted, cm.MessagesDeleted)
				assert.Equal(t, tt.expected.MessagesTruncated, cm.MessagesTruncated)
				assert.Equal(t, tt.expected.ToolUsesCleared, cm.ToolUsesCleared)
				assert.Equal(t, tt.expected.ThinkingBlocksCleared, cm.ThinkingBlocksCleared)
			}
		})
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}

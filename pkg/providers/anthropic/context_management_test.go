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
			name: "clear_tool_uses with basic config",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewClearToolUsesEdit(),
				},
			},
			expected: `{"edits":[{"type":"clear_tool_uses_20250919"}]}`,
		},
		{
			name: "clear_tool_uses with full config",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewClearToolUsesEdit().
						WithInputTokensTrigger(10000).
						WithKeepToolUses(3).
						WithClearAtLeast(5000).
						WithExcludeTools("search", "calculator"),
				},
			},
			expected: `{
				"edits": [{
					"type": "clear_tool_uses_20250919",
					"trigger": {"type": "input_tokens", "value": 10000},
					"keep": {"type": "tool_uses", "value": 3},
					"clearAtLeast": {"type": "input_tokens", "value": 5000},
					"excludeTools": ["search", "calculator"]
				}]
			}`,
		},
		{
			name: "clear_thinking with keep all",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewClearThinkingEdit().WithKeepAll(),
				},
			},
			expected: `{
				"edits": [{
					"type": "clear_thinking_20251015",
					"keep": "all"
				}]
			}`,
		},
		{
			name: "clear_thinking with keep recent turns",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewClearThinkingEdit().WithKeepRecentTurns(2),
				},
			},
			expected: `{
				"edits": [{
					"type": "clear_thinking_20251015",
					"keep": {"type": "thinking_turns", "value": 2}
				}]
			}`,
		},
		{
			name: "compact with basic config",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewCompactEdit(),
				},
			},
			expected: `{"edits":[{"type":"compact_20260112"}]}`,
		},
		{
			name: "compact with full config",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewCompactEdit().
						WithTrigger(50000).
						WithPauseAfterCompaction(true).
						WithInstructions("Keep recent context"),
				},
			},
			expected: `{
				"edits": [{
					"type": "compact_20260112",
					"trigger": {"type": "input_tokens", "value": 50000},
					"pauseAfterCompaction": true,
					"instructions": "Keep recent context"
				}]
			}`,
		},
		{
			name: "multiple edits",
			cm: ContextManagement{
				Edits: []ContextManagementEdit{
					NewClearToolUsesEdit().WithInputTokensTrigger(10000),
					NewClearThinkingEdit().WithKeepRecentTurns(1),
					NewCompactEdit().WithTrigger(50000),
				},
			},
			expected: `{
				"edits": [
					{
						"type": "clear_tool_uses_20250919",
						"trigger": {"type": "input_tokens", "value": 10000}
					},
					{
						"type": "clear_thinking_20251015",
						"keep": {"type": "thinking_turns", "value": 1}
					},
					{
						"type": "compact_20260112",
						"trigger": {"type": "input_tokens", "value": 50000}
					}
				]
			}`,
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
		validate func(*testing.T, *ContextManagementResponse)
	}{
		{
			name: "clear_tool_uses applied",
			jsonData: `{
				"applied_edits": [{
					"type": "clear_tool_uses_20250919",
					"cleared_tool_uses": 5,
					"cleared_input_tokens": 1500
				}]
			}`,
			validate: func(t *testing.T, cmr *ContextManagementResponse) {
				require.Len(t, cmr.AppliedEdits, 1)
				edit, ok := cmr.AppliedEdits[0].(*AppliedClearToolUsesEdit)
				require.True(t, ok)
				assert.Equal(t, "clear_tool_uses_20250919", edit.Type)
				assert.Equal(t, 5, edit.ClearedToolUses)
				assert.Equal(t, 1500, edit.ClearedInputTokens)
			},
		},
		{
			name: "clear_thinking applied",
			jsonData: `{
				"applied_edits": [{
					"type": "clear_thinking_20251015",
					"cleared_thinking_turns": 3,
					"cleared_input_tokens": 2000
				}]
			}`,
			validate: func(t *testing.T, cmr *ContextManagementResponse) {
				require.Len(t, cmr.AppliedEdits, 1)
				edit, ok := cmr.AppliedEdits[0].(*AppliedClearThinkingEdit)
				require.True(t, ok)
				assert.Equal(t, "clear_thinking_20251015", edit.Type)
				assert.Equal(t, 3, edit.ClearedThinkingTurns)
				assert.Equal(t, 2000, edit.ClearedInputTokens)
			},
		},
		{
			name: "compact applied",
			jsonData: `{
				"applied_edits": [{
					"type": "compact_20260112"
				}]
			}`,
			validate: func(t *testing.T, cmr *ContextManagementResponse) {
				require.Len(t, cmr.AppliedEdits, 1)
				edit, ok := cmr.AppliedEdits[0].(*AppliedCompactEdit)
				require.True(t, ok)
				assert.Equal(t, "compact_20260112", edit.Type)
			},
		},
		{
			name: "multiple edits applied",
			jsonData: `{
				"applied_edits": [
					{
						"type": "clear_tool_uses_20250919",
						"cleared_tool_uses": 3,
						"cleared_input_tokens": 1000
					},
					{
						"type": "clear_thinking_20251015",
						"cleared_thinking_turns": 2,
						"cleared_input_tokens": 800
					}
				]
			}`,
			validate: func(t *testing.T, cmr *ContextManagementResponse) {
				require.Len(t, cmr.AppliedEdits, 2)

				edit1, ok := cmr.AppliedEdits[0].(*AppliedClearToolUsesEdit)
				require.True(t, ok)
				assert.Equal(t, 3, edit1.ClearedToolUses)
				assert.Equal(t, 1000, edit1.ClearedInputTokens)

				edit2, ok := cmr.AppliedEdits[1].(*AppliedClearThinkingEdit)
				require.True(t, ok)
				assert.Equal(t, 2, edit2.ClearedThinkingTurns)
				assert.Equal(t, 800, edit2.ClearedInputTokens)
			},
		},
		{
			name: "empty applied_edits",
			jsonData: `{
				"applied_edits": []
			}`,
			validate: func(t *testing.T, cmr *ContextManagementResponse) {
				assert.Len(t, cmr.AppliedEdits, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result ContextManagementResponse
			err := json.Unmarshal([]byte(tt.jsonData), &result)
			require.NoError(t, err)
			tt.validate(t, &result)
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
		"model": "claude-sonnet-4-5",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		},
		"context_management": {
			"applied_edits": [{
				"type": "clear_tool_uses_20250919",
				"cleared_tool_uses": 5,
				"cleared_input_tokens": 1500
			}]
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)
	require.NotNil(t, response.ContextManagement)
	require.Len(t, response.ContextManagement.AppliedEdits, 1)

	edit, ok := response.ContextManagement.AppliedEdits[0].(*AppliedClearToolUsesEdit)
	require.True(t, ok)
	assert.Equal(t, 5, edit.ClearedToolUses)
	assert.Equal(t, 1500, edit.ClearedInputTokens)
}

// TestParseContextManagement_UsageLevel tests parsing context management from usage block (legacy)
func TestParseContextManagement_UsageLevel(t *testing.T) {
	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello"}],
		"model": "claude-sonnet-4-5",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"context_management": {
				"applied_edits": [{
					"type": "clear_thinking_20251015",
					"cleared_thinking_turns": 3,
					"cleared_input_tokens": 2000
				}]
			}
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)
	require.NotNil(t, response.Usage.ContextManagement)
	require.Len(t, response.Usage.ContextManagement.AppliedEdits, 1)

	edit, ok := response.Usage.ContextManagement.AppliedEdits[0].(*AppliedClearThinkingEdit)
	require.True(t, ok)
	assert.Equal(t, 3, edit.ClearedThinkingTurns)
	assert.Equal(t, 2000, edit.ClearedInputTokens)
}

// TestParseContextManagement_RootLevelTakesPrecedence tests that root level is preferred
func TestParseContextManagement_RootLevelTakesPrecedence(t *testing.T) {
	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello"}],
		"model": "claude-sonnet-4-5",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"context_management": {
				"applied_edits": [{
					"type": "clear_tool_uses_20250919",
					"cleared_tool_uses": 1,
					"cleared_input_tokens": 100
				}]
			}
		},
		"context_management": {
			"applied_edits": [{
				"type": "clear_tool_uses_20250919",
				"cleared_tool_uses": 5,
				"cleared_input_tokens": 1500
			}]
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)

	// Both should be parsed
	require.NotNil(t, response.ContextManagement)
	require.NotNil(t, response.Usage.ContextManagement)

	// Root level should have correct values
	require.Len(t, response.ContextManagement.AppliedEdits, 1)
	rootEdit, ok := response.ContextManagement.AppliedEdits[0].(*AppliedClearToolUsesEdit)
	require.True(t, ok)
	assert.Equal(t, 5, rootEdit.ClearedToolUses)
	assert.Equal(t, 1500, rootEdit.ClearedInputTokens)

	// Usage level should also have its values
	require.Len(t, response.Usage.ContextManagement.AppliedEdits, 1)
	usageEdit, ok := response.Usage.ContextManagement.AppliedEdits[0].(*AppliedClearToolUsesEdit)
	require.True(t, ok)
	assert.Equal(t, 1, usageEdit.ClearedToolUses)
	assert.Equal(t, 100, usageEdit.ClearedInputTokens)
}

// TestGetBetaHeaders tests the beta header generation logic
func TestGetBetaHeaders(t *testing.T) {
	tests := []struct {
		name     string
		options  *ModelOptions
		expected string
	}{
		{
			name:     "no context management",
			options:  nil,
			expected: "",
		},
		{
			name: "clear_tool_uses only",
			options: &ModelOptions{
				ContextManagement: &ContextManagement{
					Edits: []ContextManagementEdit{
						NewClearToolUsesEdit(),
					},
				},
			},
			expected: BetaHeaderContextManagement,
		},
		{
			name: "clear_thinking only",
			options: &ModelOptions{
				ContextManagement: &ContextManagement{
					Edits: []ContextManagementEdit{
						NewClearThinkingEdit(),
					},
				},
			},
			expected: BetaHeaderContextManagement,
		},
		{
			name: "compact only",
			options: &ModelOptions{
				ContextManagement: &ContextManagement{
					Edits: []ContextManagementEdit{
						NewCompactEdit(),
					},
				},
			},
			expected: BetaHeaderContextManagement + "," + BetaHeaderCompact,
		},
		{
			name: "all three edit types",
			options: &ModelOptions{
				ContextManagement: &ContextManagement{
					Edits: []ContextManagementEdit{
						NewClearToolUsesEdit(),
						NewClearThinkingEdit(),
						NewCompactEdit(),
					},
				},
			},
			expected: BetaHeaderContextManagement + "," + BetaHeaderCompact,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Config{
				APIKey: "test-key",
			})

			var model *LanguageModel
			if tt.options != nil {
				m, err := p.LanguageModelWithOptions("claude-sonnet-4-5", tt.options)
				require.NoError(t, err)
				model = m.(*LanguageModel)
			} else {
				m, err := p.LanguageModel("claude-sonnet-4-5")
				require.NoError(t, err)
				model = m.(*LanguageModel)
			}

			result := model.getBetaHeaders()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDoGenerate_WithContextManagement tests non-streaming generation with context management
func TestDoGenerate_WithContextManagement(t *testing.T) {
	tests := []struct {
		name          string
		edits         []ContextManagementEdit
		expectedBeta  string
		responseJSON  string
		validateResult func(*testing.T, *types.GenerateResult)
	}{
		{
			name: "clear_tool_uses edit",
			edits: []ContextManagementEdit{
				NewClearToolUsesEdit().WithInputTokensTrigger(10000),
			},
			expectedBeta: BetaHeaderContextManagement,
			responseJSON: `{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Response"}],
				"model": "claude-sonnet-4-5",
				"stop_reason": "end_turn",
				"usage": {
					"input_tokens": 100,
					"output_tokens": 50
				},
				"context_management": {
					"applied_edits": [{
						"type": "clear_tool_uses_20250919",
						"cleared_tool_uses": 5,
						"cleared_input_tokens": 1500
					}]
				}
			}`,
			validateResult: func(t *testing.T, result *types.GenerateResult) {
				require.NotNil(t, result.ContextManagement)
				cmr, ok := result.ContextManagement.(*ContextManagementResponse)
				require.True(t, ok)
				require.Len(t, cmr.AppliedEdits, 1)

				edit, ok := cmr.AppliedEdits[0].(*AppliedClearToolUsesEdit)
				require.True(t, ok)
				assert.Equal(t, 5, edit.ClearedToolUses)
				assert.Equal(t, 1500, edit.ClearedInputTokens)
			},
		},
		{
			name: "compact edit with pause",
			edits: []ContextManagementEdit{
				NewCompactEdit().WithTrigger(50000).WithPauseAfterCompaction(true),
			},
			expectedBeta: BetaHeaderContextManagement + "," + BetaHeaderCompact,
			responseJSON: `{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "Response"}],
				"model": "claude-sonnet-4-5",
				"stop_reason": "end_turn",
				"usage": {
					"input_tokens": 100,
					"output_tokens": 50
				},
				"context_management": {
					"applied_edits": [{
						"type": "compact_20260112"
					}]
				}
			}`,
			validateResult: func(t *testing.T, result *types.GenerateResult) {
				require.NotNil(t, result.ContextManagement)
				cmr, ok := result.ContextManagement.(*ContextManagementResponse)
				require.True(t, ok)
				require.Len(t, cmr.AppliedEdits, 1)

				edit, ok := cmr.AppliedEdits[0].(*AppliedCompactEdit)
				require.True(t, ok)
				assert.Equal(t, "compact_20260112", edit.Type)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify beta header
				betaHeader := r.Header.Get("anthropic-beta")
				assert.Equal(t, tt.expectedBeta, betaHeader)

				// Parse request body
				var reqBody map[string]interface{}
				err := json.NewDecoder(r.Body).Decode(&reqBody)
				require.NoError(t, err)

				// Verify context_management is in request
				_, ok := reqBody["context_management"]
				require.True(t, ok)

				// Return mock response
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tt.responseJSON))
			}))
			defer server.Close()

			// Create provider with test server
			p := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})

			// Create model with context management
			model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &ModelOptions{
				ContextManagement: &ContextManagement{
					Edits: tt.edits,
				},
			})
			require.NoError(t, err)

			// Generate text
			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{
					Text: "Test prompt",
				},
				MaxTokens: intPtr(100),
			})
			require.NoError(t, err)
			require.NotNil(t, result)

			tt.validateResult(t, result)
		})
	}
}

// TestUsageIterations tests that usage iterations are properly parsed and summed
func TestUsageIterations(t *testing.T) {
	responseJSON := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Response"}],
		"model": "claude-sonnet-4-5",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"iterations": [
				{
					"type": "compaction",
					"input_tokens": 5000,
					"output_tokens": 100
				},
				{
					"type": "message",
					"input_tokens": 3000,
					"output_tokens": 50
				}
			]
		},
		"context_management": {
			"applied_edits": [{
				"type": "compact_20260112"
			}]
		}
	}`

	var response anthropicResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	require.NoError(t, err)

	result := &LanguageModel{}
	converted := result.convertResponse(response, false)

	// Verify that usage is summed across iterations
	require.NotNil(t, converted.Usage.InputTokens)
	require.NotNil(t, converted.Usage.OutputTokens)

	// Total input: 5000 + 3000 = 8000
	assert.Equal(t, int64(8000), *converted.Usage.InputTokens)
	// Total output: 100 + 50 = 150
	assert.Equal(t, int64(150), *converted.Usage.OutputTokens)
}

// Helper function
func intPtr(i int) *int {
	return &i
}

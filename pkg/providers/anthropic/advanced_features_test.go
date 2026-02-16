package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestFastModeConfiguration tests fast mode configuration
func TestFastModeConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		speed          Speed
		wantBetaHeader bool
	}{
		{
			name:           "fast mode enabled",
			speed:          SpeedFast,
			wantBetaHeader: true,
		},
		{
			name:           "standard mode",
			speed:          SpeedStandard,
			wantBetaHeader: false,
		},
		{
			name:           "empty speed",
			speed:          "",
			wantBetaHeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create model with speed option
			cfg := Config{
				APIKey:  "test-key",
				BaseURL: DefaultBaseURL,
			}
			prov := New(cfg)

			options := &ModelOptions{
				Speed: tt.speed,
			}

			model := NewLanguageModel(prov, "claude-opus-4-6", options)

			// Test beta header
			betaHeader := model.getBetaHeaders()
			hasBetaHeader := betaHeader != ""

			if hasBetaHeader != tt.wantBetaHeader {
				t.Errorf("getBetaHeaders() beta header presence = %v, want %v", hasBetaHeader, tt.wantBetaHeader)
			}

			if tt.wantBetaHeader && betaHeader != BetaHeaderFastMode {
				t.Errorf("getBetaHeaders() = %v, want %v", betaHeader, BetaHeaderFastMode)
			}

			// Test request body includes speed
			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					Text: "Test prompt",
				},
				MaxTokens: intPtr(100),
			}

			body := model.buildRequestBody(opts, false)

			if tt.speed != "" {
				if speedVal, ok := body["speed"]; !ok {
					t.Error("buildRequestBody() missing speed field")
				} else if speedVal != string(tt.speed) {
					t.Errorf("buildRequestBody() speed = %v, want %v", speedVal, string(tt.speed))
				}
			} else {
				if _, ok := body["speed"]; ok {
					t.Error("buildRequestBody() should not include speed when empty")
				}
			}
		})
	}
}

// TestAdaptiveThinkingConfiguration tests adaptive thinking configuration
func TestAdaptiveThinkingConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		thinking        *ThinkingConfig
		wantThinkingKey bool
		wantType        string
		wantBudget      bool
	}{
		{
			name: "adaptive thinking",
			thinking: &ThinkingConfig{
				Type: ThinkingTypeAdaptive,
			},
			wantThinkingKey: true,
			wantType:        "adaptive",
			wantBudget:      false,
		},
		{
			name: "enabled thinking with budget",
			thinking: &ThinkingConfig{
				Type:         ThinkingTypeEnabled,
				BudgetTokens: intPtr(5000),
			},
			wantThinkingKey: true,
			wantType:        "enabled",
			wantBudget:      true,
		},
		{
			name: "enabled thinking without budget",
			thinking: &ThinkingConfig{
				Type: ThinkingTypeEnabled,
			},
			wantThinkingKey: true,
			wantType:        "enabled",
			wantBudget:      false,
		},
		{
			name: "disabled thinking",
			thinking: &ThinkingConfig{
				Type: ThinkingTypeDisabled,
			},
			wantThinkingKey: true,
			wantType:        "disabled",
			wantBudget:      false,
		},
		{
			name:            "no thinking config",
			thinking:        nil,
			wantThinkingKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create model with thinking option
			cfg := Config{
				APIKey:  "test-key",
				BaseURL: DefaultBaseURL,
			}
			prov := New(cfg)

			options := &ModelOptions{
				Thinking: tt.thinking,
			}

			model := NewLanguageModel(prov, "claude-sonnet-4", options)

			// Test request body includes thinking config
			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					Text: "Complex problem",
				},
				MaxTokens: intPtr(100),
			}

			body := model.buildRequestBody(opts, false)

			thinkingVal, hasThinking := body["thinking"]
			if hasThinking != tt.wantThinkingKey {
				t.Errorf("buildRequestBody() thinking presence = %v, want %v", hasThinking, tt.wantThinkingKey)
				return
			}

			if tt.wantThinkingKey {
				thinkingMap, ok := thinkingVal.(map[string]interface{})
				if !ok {
					t.Fatal("thinking value is not a map")
				}

				if thinkingMap["type"] != tt.wantType {
					t.Errorf("thinking type = %v, want %v", thinkingMap["type"], tt.wantType)
				}

				_, hasBudget := thinkingMap["budget_tokens"]
				if hasBudget != tt.wantBudget {
					t.Errorf("thinking budget_tokens presence = %v, want %v", hasBudget, tt.wantBudget)
				}

				if tt.wantBudget {
					if thinkingMap["budget_tokens"] != *tt.thinking.BudgetTokens {
						t.Errorf("thinking budget_tokens = %v, want %v", thinkingMap["budget_tokens"], *tt.thinking.BudgetTokens)
					}
				}
			}
		})
	}
}

// TestCombinedFeaturesConfiguration tests combining multiple features
func TestCombinedFeaturesConfiguration(t *testing.T) {
	cfg := Config{
		APIKey:  "test-key",
		BaseURL: DefaultBaseURL,
	}
	prov := New(cfg)

	options := &ModelOptions{
		Speed: SpeedFast,
		Thinking: &ThinkingConfig{
			Type: ThinkingTypeAdaptive,
		},
	}

	model := NewLanguageModel(prov, "claude-opus-4-6", options)

	// Test beta header includes fast mode
	betaHeader := model.getBetaHeaders()
	if betaHeader != BetaHeaderFastMode {
		t.Errorf("getBetaHeaders() = %v, want %v", betaHeader, BetaHeaderFastMode)
	}

	// Test request body includes both speed and thinking
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Test prompt",
		},
		MaxTokens: intPtr(100),
	}

	body := model.buildRequestBody(opts, false)

	// Check speed
	if speedVal, ok := body["speed"]; !ok {
		t.Error("buildRequestBody() missing speed field")
	} else if speedVal != "fast" {
		t.Errorf("buildRequestBody() speed = %v, want fast", speedVal)
	}

	// Check thinking
	if thinkingVal, ok := body["thinking"]; !ok {
		t.Error("buildRequestBody() missing thinking field")
	} else {
		thinkingMap := thinkingVal.(map[string]interface{})
		if thinkingMap["type"] != "adaptive" {
			t.Errorf("buildRequestBody() thinking type = %v, want adaptive", thinkingMap["type"])
		}
	}
}

// TestThinkingContentInResponse tests handling of thinking content in responses
func TestThinkingContentInResponse(t *testing.T) {
	// Create a mock response with thinking content
	response := anthropicResponse{
		ID:   "msg_test",
		Type: "message",
		Role: "assistant",
		Content: []anthropicContent{
			{
				Type:      "thinking",
				Thinking:  "Let me think about this problem...",
				Signature: "signature123",
			},
			{
				Type: "text",
				Text: "Here is my answer.",
			},
		},
		Model:      "claude-sonnet-4",
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  10,
			OutputTokens: 20,
		},
	}

	cfg := Config{
		APIKey:  "test-key",
		BaseURL: DefaultBaseURL,
	}
	prov := New(cfg)
	model := NewLanguageModel(prov, "claude-sonnet-4", nil)

	// Convert response
	result := model.convertResponse(response)

	// Verify text is extracted correctly (should skip thinking content)
	if result.Text != "Here is my answer." {
		t.Errorf("convertResponse() text = %v, want 'Here is my answer.'", result.Text)
	}

	// Verify thinking content is in raw response
	rawResp, ok := result.RawResponse.(anthropicResponse)
	if !ok {
		t.Fatal("RawResponse is not anthropicResponse")
	}

	foundThinking := false
	for _, content := range rawResp.Content {
		if content.Type == "thinking" {
			foundThinking = true
			if content.Thinking != "Let me think about this problem..." {
				t.Errorf("thinking content = %v, want 'Let me think about this problem...'", content.Thinking)
			}
			break
		}
	}

	if !foundThinking {
		t.Error("thinking content not found in raw response")
	}
}

// TestResponseBodySerialization tests that the request body can be serialized to JSON
func TestResponseBodySerialization(t *testing.T) {
	cfg := Config{
		APIKey:  "test-key",
		BaseURL: DefaultBaseURL,
	}
	prov := New(cfg)

	budget := 5000
	options := &ModelOptions{
		Speed: SpeedFast,
		Thinking: &ThinkingConfig{
			Type:         ThinkingTypeEnabled,
			BudgetTokens: &budget,
		},
	}

	model := NewLanguageModel(prov, "claude-opus-4-6", options)

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Test prompt",
		},
		MaxTokens: intPtr(100),
	}

	body := model.buildRequestBody(opts, false)

	// Try to serialize to JSON
	_, err := json.Marshal(body)
	if err != nil {
		t.Errorf("failed to marshal request body to JSON: %v", err)
	}
}


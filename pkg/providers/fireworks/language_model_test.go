package fireworks

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestBuildRequestBodyWithThinking(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/kimi-k2p5")

	tests := []struct {
		name           string
		providerOpts   map[string]interface{}
		expectedBody   map[string]interface{}
		checkThinking  bool
		checkReasoning bool
	}{
		{
			name: "thinking enabled with budget",
			providerOpts: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":         "enabled",
					"budgetTokens": 2048,
				},
			},
			expectedBody: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":          "enabled",
					"budget_tokens": 2048,
				},
			},
			checkThinking: true,
		},
		{
			name: "thinking enabled without budget",
			providerOpts: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type": "enabled",
				},
			},
			expectedBody: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type": "enabled",
				},
			},
			checkThinking: true,
		},
		{
			name: "thinking disabled",
			providerOpts: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type": "disabled",
				},
			},
			expectedBody: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type": "disabled",
				},
			},
			checkThinking: true,
		},
		{
			name: "reasoning history interleaved",
			providerOpts: map[string]interface{}{
				"reasoningHistory": "interleaved",
			},
			expectedBody: map[string]interface{}{
				"reasoning_history": "interleaved",
			},
			checkReasoning: true,
		},
		{
			name: "reasoning history preserved",
			providerOpts: map[string]interface{}{
				"reasoningHistory": "preserved",
			},
			expectedBody: map[string]interface{}{
				"reasoning_history": "preserved",
			},
			checkReasoning: true,
		},
		{
			name: "reasoning history disabled",
			providerOpts: map[string]interface{}{
				"reasoningHistory": "disabled",
			},
			expectedBody: map[string]interface{}{
				"reasoning_history": "disabled",
			},
			checkReasoning: true,
		},
		{
			name: "thinking and reasoning together",
			providerOpts: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":         "enabled",
					"budgetTokens": 3072,
				},
				"reasoningHistory": "preserved",
			},
			expectedBody: map[string]interface{}{
				"thinking": map[string]interface{}{
					"type":          "enabled",
					"budget_tokens": 3072,
				},
				"reasoning_history": "preserved",
			},
			checkThinking:  true,
			checkReasoning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					Text: "Test prompt",
				},
				ProviderOptions: tt.providerOpts,
			}

			body := model.buildRequestBody(opts, false)

			if tt.checkThinking {
				thinking, ok := body["thinking"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected thinking in body, got none")
				}

				expectedThinking := tt.expectedBody["thinking"].(map[string]interface{})

				if thinkingType, ok := expectedThinking["type"].(string); ok {
					if thinking["type"] != thinkingType {
						t.Errorf("thinking type mismatch: expected %s, got %s",
							thinkingType, thinking["type"])
					}
				}

				if budgetTokens, ok := expectedThinking["budget_tokens"].(int); ok {
					if thinking["budget_tokens"] != budgetTokens {
						t.Errorf("budget_tokens mismatch: expected %d, got %v",
							budgetTokens, thinking["budget_tokens"])
					}
				}
			}

			if tt.checkReasoning {
				reasoningHistory, ok := body["reasoning_history"].(string)
				if !ok {
					t.Fatalf("expected reasoning_history in body, got none")
				}

				expectedReasoning := tt.expectedBody["reasoning_history"].(string)
				if reasoningHistory != expectedReasoning {
					t.Errorf("reasoning_history mismatch: expected %s, got %s",
						expectedReasoning, reasoningHistory)
				}
			}
		})
	}
}

func TestBuildRequestBodyWithoutProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/kimi-k2p5")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Test prompt",
		},
		// No ProviderOptions
	}

	body := model.buildRequestBody(opts, false)

	// Verify thinking and reasoning_history are not present
	if _, ok := body["thinking"]; ok {
		t.Error("thinking should not be present without provider options")
	}
	if _, ok := body["reasoning_history"]; ok {
		t.Error("reasoning_history should not be present without provider options")
	}

	// Verify basic fields are still present
	if body["model"] != "accounts/fireworks/models/kimi-k2p5" {
		t.Errorf("model field incorrect: %v", body["model"])
	}
	if _, ok := body["messages"]; !ok {
		t.Error("messages field should be present")
	}
}

func TestCamelCaseToSnakeCaseConversion(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "accounts/fireworks/models/kimi-k2p5")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: "Test prompt",
		},
		ProviderOptions: map[string]interface{}{
			"thinking": map[string]interface{}{
				"type":         "enabled",
				"budgetTokens": 4096, // camelCase
			},
			"reasoningHistory": "interleaved", // camelCase
		},
	}

	body := model.buildRequestBody(opts, false)

	// Verify snake_case conversion
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatal("thinking not found in body")
	}

	if _, ok := thinking["budget_tokens"]; !ok {
		t.Error("expected budget_tokens (snake_case), got none")
	}

	// budgetTokens (camelCase) should NOT exist in the final body
	if _, ok := thinking["budgetTokens"]; ok {
		t.Error("budgetTokens (camelCase) should not be in API request")
	}

	// Verify reasoningHistory -> reasoning_history
	if _, ok := body["reasoning_history"]; !ok {
		t.Error("expected reasoning_history (snake_case), got none")
	}

	// reasoningHistory (camelCase) should NOT exist in the final body
	if _, ok := body["reasoningHistory"]; ok {
		t.Error("reasoningHistory (camelCase) should not be in API request")
	}
}

func TestThinkingOptionsHelpers(t *testing.T) {
	t.Run("WithThinking enabled", func(t *testing.T) {
		opts := WithThinking(true)
		if opts.Type != "enabled" {
			t.Errorf("expected Type=enabled, got %s", opts.Type)
		}
	})

	t.Run("WithThinking disabled", func(t *testing.T) {
		opts := WithThinking(false)
		if opts.Type != "disabled" {
			t.Errorf("expected Type=disabled, got %s", opts.Type)
		}
	})

	t.Run("WithThinkingBudget", func(t *testing.T) {
		opts := WithThinkingBudget(2048)
		if opts.Type != "enabled" {
			t.Errorf("expected Type=enabled, got %s", opts.Type)
		}
		if opts.BudgetTokens == nil || *opts.BudgetTokens != 2048 {
			t.Errorf("expected BudgetTokens=2048, got %v", opts.BudgetTokens)
		}
	})

	t.Run("WithThinkingBudget minimum enforcement", func(t *testing.T) {
		opts := WithThinkingBudget(512) // Below minimum of 1024
		if opts.BudgetTokens == nil || *opts.BudgetTokens != 1024 {
			t.Errorf("expected BudgetTokens=1024 (minimum), got %v", opts.BudgetTokens)
		}
	})

	t.Run("WithReasoningHistory", func(t *testing.T) {
		opts := WithReasoningHistory("preserved")
		if opts.ReasoningHistory != "preserved" {
			t.Errorf("expected ReasoningHistory=preserved, got %s", opts.ReasoningHistory)
		}
	})
}

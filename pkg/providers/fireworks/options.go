package fireworks

// ThinkingOptions configures thinking behavior for Kimi K2.5 and similar models
type ThinkingOptions struct {
	// Type controls whether thinking is enabled or disabled
	// Valid values: "enabled", "disabled"
	Type string `json:"type,omitempty"`

	// BudgetTokens sets the maximum number of tokens allocated for reasoning
	// Minimum value: 1024
	BudgetTokens *int `json:"budgetTokens,omitempty"`
}

// FireworksLanguageModelOptions contains Fireworks-specific model options
// These options are used for models like Kimi K2.5 that support extended reasoning
type FireworksLanguageModelOptions struct {
	// Thinking configures extended reasoning capabilities
	Thinking *ThinkingOptions `json:"thinking,omitempty"`

	// ReasoningHistory controls how intermediate reasoning steps are handled
	// Valid values: "disabled", "interleaved", "preserved"
	//
	// - "disabled": Don't include reasoning history
	// - "interleaved": Mix reasoning with response generation
	// - "preserved": Keep reasoning separate from response
	ReasoningHistory string `json:"reasoningHistory,omitempty"`
}

// WithThinking is a helper to create thinking options with enabled state
func WithThinking(enabled bool) *ThinkingOptions {
	thinkingType := "disabled"
	if enabled {
		thinkingType = "enabled"
	}
	return &ThinkingOptions{
		Type: thinkingType,
	}
}

// WithThinkingBudget creates thinking options with a specific token budget
func WithThinkingBudget(budgetTokens int) *ThinkingOptions {
	if budgetTokens < 1024 {
		budgetTokens = 1024
	}
	return &ThinkingOptions{
		Type:         "enabled",
		BudgetTokens: &budgetTokens,
	}
}

// WithReasoningHistory creates options with a specific reasoning history mode
func WithReasoningHistory(mode string) *FireworksLanguageModelOptions {
	return &FireworksLanguageModelOptions{
		ReasoningHistory: mode,
	}
}

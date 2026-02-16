package anthropic

// ThinkingType represents the type of thinking configuration
type ThinkingType string

const (
	// ThinkingTypeAdaptive enables adaptive thinking (Opus 4.6+)
	// Claude dynamically adjusts reasoning effort based on the task complexity.
	ThinkingTypeAdaptive ThinkingType = "adaptive"

	// ThinkingTypeEnabled enables extended thinking with optional budget (pre-Opus 4.6)
	// Responses include thinking content blocks showing Claude's reasoning process.
	ThinkingTypeEnabled ThinkingType = "enabled"

	// ThinkingTypeDisabled disables thinking
	ThinkingTypeDisabled ThinkingType = "disabled"
)

// Speed represents the inference speed mode
type Speed string

const (
	// SpeedFast enables fast mode for 2.5x faster output token speeds
	// Only supported with claude-opus-4-6
	SpeedFast Speed = "fast"

	// SpeedStandard uses standard inference speed (default)
	SpeedStandard Speed = "standard"
)

// ThinkingConfig configures Claude's extended thinking capabilities
type ThinkingConfig struct {
	// Type specifies the thinking mode
	Type ThinkingType `json:"type"`

	// BudgetTokens specifies the maximum tokens for thinking (only for "enabled" type)
	// Requires a minimum of 1,024 tokens and counts towards the max_tokens limit.
	// Optional for "enabled" type, not used for "adaptive" type.
	BudgetTokens *int `json:"budget_tokens,omitempty"`
}

// ModelOptions contains optional configuration for Anthropic language models.
// These options can be passed when creating a model instance to configure
// provider-specific features.
type ModelOptions struct {
	// ContextManagement enables automatic cleanup of conversation history
	// to prevent context window overflow in long conversations.
	//
	// This is a beta feature that requires Claude 4.5+ models.
	// When enabled, Anthropic will automatically remove or truncate old
	// content based on the configured strategies.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       ContextManagement: &anthropic.ContextManagement{
	//           Strategies: []string{anthropic.StrategyClearToolUses},
	//       },
	//   }
	//
	// See ContextManagement for available strategies.
	ContextManagement *ContextManagement `json:"context_management,omitempty"`

	// Thinking configures Claude's extended thinking capabilities.
	//
	// When enabled, responses include thinking content blocks showing
	// Claude's reasoning process before the final answer.
	//
	// For Opus 4.6 and newer models, use ThinkingTypeAdaptive:
	//   options := anthropic.ModelOptions{
	//       Thinking: &anthropic.ThinkingConfig{
	//           Type: anthropic.ThinkingTypeAdaptive,
	//       },
	//   }
	//
	// For models before Opus 4.6, use ThinkingTypeEnabled with optional budget:
	//   budget := 5000
	//   options := anthropic.ModelOptions{
	//       Thinking: &anthropic.ThinkingConfig{
	//           Type: anthropic.ThinkingTypeEnabled,
	//           BudgetTokens: &budget,
	//       },
	//   }
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Speed configures the inference speed mode.
	//
	// Fast mode provides 2.5x faster output token speeds but is only
	// supported with claude-opus-4-6.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       Speed: anthropic.SpeedFast,
	//   }
	Speed Speed `json:"speed,omitempty"`
}

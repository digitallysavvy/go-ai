package bedrock

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

// ThinkingConfig configures Claude's extended thinking capabilities
type ThinkingConfig struct {
	// Type specifies the thinking mode
	Type ThinkingType `json:"type"`

	// BudgetTokens specifies the maximum tokens for thinking (only for "enabled" type)
	// Requires a minimum of 1,024 tokens and counts towards the max_tokens limit.
	// Optional for "enabled" type, not used for "adaptive" type.
	BudgetTokens *int `json:"budget_tokens,omitempty"`
}

// ReasoningConfig mirrors the TypeScript amazonBedrock.reasoningConfig option.
type ReasoningConfig struct {
	Type               string `json:"type,omitempty"`
	BudgetTokens       *int   `json:"budgetTokens,omitempty"`
	MaxReasoningEffort string `json:"maxReasoningEffort,omitempty"`
	Display            string `json:"display,omitempty"`
}

// ModelOptions contains optional configuration for AWS Bedrock Claude models.
// These options can be passed when creating a model instance to configure
// provider-specific features for Claude models running on Bedrock.
type ModelOptions struct {
	// Thinking configures Claude's extended thinking capabilities.
	//
	// When enabled, responses include thinking content blocks showing
	// Claude's reasoning process before the final answer.
	//
	// For Opus 4.6 and newer models, use ThinkingTypeAdaptive:
	//   options := bedrock.ModelOptions{
	//       Thinking: &bedrock.ThinkingConfig{
	//           Type: bedrock.ThinkingTypeAdaptive,
	//       },
	//   }
	//
	// For models before Opus 4.6, use ThinkingTypeEnabled with optional budget:
	//   budget := 5000
	//   options := bedrock.ModelOptions{
	//       Thinking: &bedrock.ThinkingConfig{
	//           Type: bedrock.ThinkingTypeEnabled,
	//           BudgetTokens: &budget,
	//       },
	//   }
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// ReasoningConfig configures Bedrock model-specific reasoning fields.
	// Partial configs are merged with values derived from top-level Reasoning.
	ReasoningConfig *ReasoningConfig `json:"reasoningConfig,omitempty"`

	// AdditionalModelRequestFields are provider-specific fields forwarded into
	// the request body after SDK-derived fields are applied.
	AdditionalModelRequestFields map[string]interface{} `json:"additionalModelRequestFields,omitempty"`

	// ServiceTier selects Bedrock service tier for inference: reserved,
	// priority, default, or flex.
	ServiceTier string `json:"serviceTier,omitempty"`
}

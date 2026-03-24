package types

// ReasoningLevel controls how much reasoning effort the model applies.
// It is a typed string to prevent bare string comparisons across the codebase.
//
// Providers map levels to their native APIs:
//   - Anthropic: thinking.budget_tokens
//   - OpenAI: reasoning_effort
//   - Amazon Bedrock: reasoningConfig (Anthropic-style) or reasoning_effort (Nova)
//   - Google/Vertex: generationConfig.thinkingConfig.thinkingBudget (dynamic %)
type ReasoningLevel string

const (
	// ReasoningDefault means "do not override the provider's own default".
	// The reasoning field is omitted from API requests entirely.
	ReasoningDefault ReasoningLevel = "provider-default"

	// ReasoningNone disables reasoning/thinking entirely.
	ReasoningNone ReasoningLevel = "none"

	// ReasoningMinimal enables minimal reasoning (Anthropic: 1024 budget tokens).
	ReasoningMinimal ReasoningLevel = "minimal"

	// ReasoningLow enables low reasoning effort (Anthropic: 4000 budget tokens).
	ReasoningLow ReasoningLevel = "low"

	// ReasoningMedium enables medium reasoning effort (Anthropic: 10000 budget tokens).
	ReasoningMedium ReasoningLevel = "medium"

	// ReasoningHigh enables high reasoning effort (Anthropic: 16000 budget tokens).
	ReasoningHigh ReasoningLevel = "high"

	// ReasoningXHigh enables maximum reasoning effort (Anthropic: 32000 budget tokens).
	ReasoningXHigh ReasoningLevel = "xhigh"
)

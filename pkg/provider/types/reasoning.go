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

	// ReasoningMinimal enables minimal reasoning (a small fraction of the
	// model's output budget; Anthropic maps it to 2% with a 1024-token floor).
	ReasoningMinimal ReasoningLevel = "minimal"

	// ReasoningLow enables low reasoning effort (Anthropic: 10% of the model's
	// maximum output tokens).
	ReasoningLow ReasoningLevel = "low"

	// ReasoningMedium enables medium reasoning effort (Anthropic: 30%).
	ReasoningMedium ReasoningLevel = "medium"

	// ReasoningHigh enables high reasoning effort (Anthropic: 60%).
	ReasoningHigh ReasoningLevel = "high"

	// ReasoningXHigh enables extra-high reasoning effort (Anthropic: 90%).
	ReasoningXHigh ReasoningLevel = "xhigh"

	// ReasoningMax enables the portable maximum reasoning level. Providers
	// with a native maximum tier send it as-is; others coerce it to their
	// highest supported tier and emit a compatibility warning.
	ReasoningMax ReasoningLevel = "max"
)

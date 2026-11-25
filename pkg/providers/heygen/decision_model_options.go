package heygen

// HeyGenDecisionModelID identifies a HeyGen Decision model. Any string is
// accepted and forwarded verbatim. Mirrors TS HeyGenDecisionModelId.
type HeyGenDecisionModelID string

const (
	// DecisionModelHeyGenDecision1 is the HeyGen Decision model.
	DecisionModelHeyGenDecision1 HeyGenDecisionModelID = "heygen-decision-1"
	// DecisionModelHeyGenDecision1Turbo is the faster HeyGen Decision model.
	DecisionModelHeyGenDecision1Turbo HeyGenDecisionModelID = "heygen-decision-1-turbo"
)

// HeyGen decision models do not support provider-specific options (TS
// HeyGenDecisionModelOptions is Record<string, never>): any
// providerOptions.heygen key is reported as an unsupported-option warning.

// heygenDecisionLimits are the adapter limits that apply to custom IDs as
// well as catalog models (TS heyGenDecisionModelLimits).
const (
	decisionMaxQuestions     = 400
	decisionMaxChoiceOptions = 255
	decisionMaxScoreLevels   = 10
	decisionMaxImages        = 4
	decisionMaxVideos        = 1
)

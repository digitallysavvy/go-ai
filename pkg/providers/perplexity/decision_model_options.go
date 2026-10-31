package perplexity

// PerplexityDecisionModelID identifies a Perplexity Decisions API model. Any
// string is accepted and forwarded verbatim (no alias resolution). Mirrors TS
// PerplexityDecisionModelId.
type PerplexityDecisionModelID string

const (
	DecisionModelDeciderV1_2            PerplexityDecisionModelID = "pplx-decider-v1.2"
	DecisionModelDeciderFlashV1_2       PerplexityDecisionModelID = "pplx-decider-flash-v1.2"
	DecisionModelDeciderLatest          PerplexityDecisionModelID = "pplx-decider-latest"
	DecisionModelDeciderFlashLatest     PerplexityDecisionModelID = "pplx-decider-flash-latest"
	DecisionModelPerplexityDecider      PerplexityDecisionModelID = "perplexity/pplx-decider-latest"
	DecisionModelPerplexityDeciderFlash PerplexityDecisionModelID = "perplexity/pplx-decider-flash-latest"
	DecisionModelDeciderV1_1_27B        PerplexityDecisionModelID = "pplx-decider-v1.1-27b"
	DecisionModelDeciderV1_27B          PerplexityDecisionModelID = "pplx-decider-v1-27b"
)

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

// DecisionModelOptions are the providerOptions.perplexity options of decision
// models (TS PerplexityDecisionModelOptions, d5440b3311).
type DecisionModelOptions struct {
	// Mode "web_grounding" searches the web before answering the questions.
	Mode string `json:"mode,omitempty"`
	// MaxStaleness is the maximum age in seconds of a fetched page (a
	// non-negative integer). It does not filter search results. Zero is sent.
	MaxStaleness *int `json:"maxStaleness,omitempty"`
}

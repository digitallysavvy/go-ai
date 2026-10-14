package provider

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DecisionQuestion is one judgment to make about the shared decision
// state. Mirrors TypeScript's Experimental_DecisionModelV4Question union
// (choice/score/boolean). Fields are interpreted according to Type; Go does
// not have discriminated unions so Criteria is typed loosely, matching how
// the TypeScript validator itself treats it structurally.
type DecisionQuestion struct {
	// Type is "choice", "score", or "boolean".
	Type string

	// Instructions is a JSON-compatible string, map[string]interface{}, or
	// []interface{}.
	Instructions interface{}

	// Criteria is interpreted according to Type:
	//   - "choice": a nonempty map[string]interface{} of option name ->
	//     description (nil description allowed, key must still be present).
	//   - "score": a []interface{} of at least two ordered levels (nil
	//     entries allowed).
	//   - "boolean": optional; when non-nil, a map[string]interface{} with
	//     only "true"/"false" keys.
	Criteria interface{}
}

// DecisionAnswer is one typed answer to an DecisionQuestion. Mirrors
// TypeScript's Experimental_DecisionModelV4Answer union.
type DecisionAnswer struct {
	// Type is "choice", "score", or "boolean".
	Type string `json:"type"`

	// Choice is the selected option for a "choice" answer.
	Choice string `json:"choice,omitempty"`

	// Score is the fractional position in [0, number of levels - 1] for a
	// "score" answer.
	Score *float64 `json:"score,omitempty"`

	// Probability is the model-estimated P(true) in [0, 1] for a "boolean"
	// answer.
	Probability *float64 `json:"probability,omitempty"`

	// Probabilities is the complete distribution over the question's
	// options/levels, when available, for "choice" and "score" answers.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// DecisionRounding declares the decimal precision a provider rounded its
// output to. Omit a field for full precision.
type DecisionRounding struct {
	ProbabilityDecimals *int `json:"probabilityDecimals,omitempty"`
	ScoreDecimals       *int `json:"scoreDecimals,omitempty"`
}

// DecisionUsage reports token usage for a decision call.
type DecisionUsage struct {
	InputTokens  *int `json:"inputTokens,omitempty"`
	OutputTokens *int `json:"outputTokens,omitempty"`
}

// DecisionResponseInfo carries raw response metadata from the provider.
type DecisionResponseInfo struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// DecisionCallOptions are passed to DecisionModel.DoDecide.
type DecisionCallOptions struct {
	// State is one shared JSON-compatible string, map[string]interface{}, or
	// []interface{}, even when the value is an array.
	State interface{}

	// Questions maps question ID -> question.
	Questions map[string]DecisionQuestion

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// DecisionResult is returned by DecisionModel.DoDecide.
type DecisionResult struct {
	// Answers has exactly one answer per question, keyed by question ID.
	Answers map[string]DecisionAnswer

	Rounding         *DecisionRounding
	Usage            *DecisionUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         *DecisionResponseInfo
}

// DecisionModel is the experimental decision model contract (V4). May
// change in patch releases. Mirrors TypeScript's Experimental_DecisionModelV4.
type DecisionModel interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// SupportedQuestionTypes lists the question types this model accepts (a
	// subset of "choice", "score", "boolean"), used to reject unsupported
	// calls before any I/O.
	SupportedQuestionTypes() []string

	// DoDecide decides every question against the same state. No partial
	// results.
	DoDecide(ctx context.Context, opts DecisionCallOptions) (*DecisionResult, error)
}

// DecisionModelProvider is a structural extension a Provider may implement
// to resolve decision models by ID. Decision is experimental and not
// part of the stable Provider contract, mirroring TypeScript's
// DecisionProvider.decisionModel.
type DecisionModelProvider interface {
	DecisionModel(modelID string) (DecisionModel, error)
}

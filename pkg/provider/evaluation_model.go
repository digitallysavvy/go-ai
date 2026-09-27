package provider

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EvaluationQuestion is one judgment to make about the shared evaluation
// state. Mirrors TypeScript's Experimental_EvaluationModelV4Question union
// (choice/score/boolean). Fields are interpreted according to Type; Go does
// not have discriminated unions so Criteria is typed loosely, matching how
// the TypeScript validator itself treats it structurally.
type EvaluationQuestion struct {
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

// EvaluationAnswer is one typed answer to an EvaluationQuestion. Mirrors
// TypeScript's Experimental_EvaluationModelV4Answer union.
type EvaluationAnswer struct {
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

// EvaluationRounding declares the decimal precision a provider rounded its
// output to. Omit a field for full precision.
type EvaluationRounding struct {
	ProbabilityDecimals *int `json:"probabilityDecimals,omitempty"`
	ScoreDecimals       *int `json:"scoreDecimals,omitempty"`
}

// EvaluationUsage reports token usage for an evaluation call.
type EvaluationUsage struct {
	InputTokens  *int `json:"inputTokens,omitempty"`
	OutputTokens *int `json:"outputTokens,omitempty"`
}

// EvaluationResponseInfo carries raw response metadata from the provider.
type EvaluationResponseInfo struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// EvaluationCallOptions are passed to EvaluationModel.DoEvaluate.
type EvaluationCallOptions struct {
	// State is one shared JSON-compatible string, map[string]interface{}, or
	// []interface{}, even when the value is an array.
	State interface{}

	// Questions maps question ID -> question.
	Questions map[string]EvaluationQuestion

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// EvaluationResult is returned by EvaluationModel.DoEvaluate.
type EvaluationResult struct {
	// Answers has exactly one answer per question, keyed by question ID.
	Answers map[string]EvaluationAnswer

	Rounding         *EvaluationRounding
	Usage            *EvaluationUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         *EvaluationResponseInfo
}

// EvaluationModel is the experimental evaluation model contract (V4). May
// change in patch releases. Mirrors TypeScript's Experimental_EvaluationModelV4.
type EvaluationModel interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// SupportedQuestionTypes lists the question types this model accepts (a
	// subset of "choice", "score", "boolean"), used to reject unsupported
	// calls before any I/O.
	SupportedQuestionTypes() []string

	// DoEvaluate evaluates every question against the same state. No partial
	// results.
	DoEvaluate(ctx context.Context, opts EvaluationCallOptions) (*EvaluationResult, error)
}

// EvaluationModelProvider is a structural extension a Provider may implement
// to resolve evaluation models by ID. Evaluation is experimental and not
// part of the stable Provider contract, mirroring TypeScript's
// EvaluationProvider.evaluationModel.
type EvaluationModelProvider interface {
	EvaluationModel(modelID string) (EvaluationModel, error)
}

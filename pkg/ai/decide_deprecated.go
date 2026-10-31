package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// This file keeps the pre-rename "evaluate" names as deprecated aliases,
// mirroring the deprecated aliases the TypeScript SDK keeps after renaming
// experimental_evaluate to experimental_decide (vercel/ai #21935).

// EvaluateUsage is an alias of DecideUsage.
//
// Deprecated: Use DecideUsage instead.
type EvaluateUsage = DecideUsage

// EvaluateResponse is an alias of DecideResponse.
//
// Deprecated: Use DecideResponse instead.
type EvaluateResponse = DecideResponse

// EvaluateOnStartEvent is an alias of DecideOnStartEvent.
//
// Deprecated: Use DecideOnStartEvent instead.
type EvaluateOnStartEvent = DecideOnStartEvent

// EvaluateOnEndEvent is an alias of DecideOnEndEvent.
//
// Deprecated: Use DecideOnEndEvent instead.
type EvaluateOnEndEvent = DecideOnEndEvent

// EvaluateResult is an alias of DecideResult.
//
// Deprecated: Use DecideResult instead.
type EvaluateResult = DecideResult

// EvaluateOptions is an alias of DecideOptions.
//
// Deprecated: Use DecideOptions instead.
type EvaluateOptions = DecideOptions

// ExperimentalEvaluate is the pre-rename form of ExperimentalDecide.
//
// Deprecated: Use ExperimentalDecide instead.
func ExperimentalEvaluate(ctx context.Context, opts EvaluateOptions) (*EvaluateResult, error) {
	return ExperimentalDecide(ctx, opts)
}

// EvaluationLanguageModel is an alias of DecisionLanguageModel.
//
// Deprecated: Use DecisionLanguageModel instead.
type EvaluationLanguageModel = DecisionLanguageModel

// EvaluationLanguageModelOptions is an alias of DecisionLanguageModelOptions.
//
// Deprecated: Use DecisionLanguageModelOptions instead.
type EvaluationLanguageModelOptions = DecisionLanguageModelOptions

// NewEvaluationLanguageModel is the pre-rename form of NewDecisionLanguageModel.
//
// Deprecated: Use NewDecisionLanguageModel instead.
func NewEvaluationLanguageModel(opts EvaluationLanguageModelOptions) (*EvaluationLanguageModel, error) {
	return NewDecisionLanguageModel(opts)
}

// DoEvaluate is the pre-rename form of DoDecide.
//
// Deprecated: Use DoDecide instead.
func (e *DecisionLanguageModel) DoEvaluate(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	return e.DoDecide(ctx, opts)
}

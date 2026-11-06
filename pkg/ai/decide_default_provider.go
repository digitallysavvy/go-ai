package ai

import (
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// defaultProviderState holds the provider set with SetDefaultProvider.
var defaultProviderState struct {
	mu       sync.RWMutex
	provider interface{}
}

// SetDefaultProvider sets the provider that resolves string model IDs when a
// call names no provider of its own: ExperimentalDecide's string Model (when
// DecideOptions has no DecisionProvider or Provider) and the batch functions
// (when their Provider is nil). It is the Go form of TS
// `globalThis.AI_SDK_DEFAULT_PROVIDER`. nil (the default) restores the AI
// Gateway.
//
// For decisions the provider must implement provider.DecisionModelProvider
// or the deprecated provider.EvaluationModelProvider; for batches a
// provider.BatchV4 or provider.BatchProvider. Otherwise those calls fail, as
// in TS. Safe for concurrent use.
func SetDefaultProvider(p interface{}) {
	defaultProviderState.mu.Lock()
	defaultProviderState.provider = p
	defaultProviderState.mu.Unlock()
}

// configuredDefaultProvider returns the provider set with SetDefaultProvider,
// or nil.
func configuredDefaultProvider() interface{} {
	defaultProviderState.mu.RLock()
	defer defaultProviderState.mu.RUnlock()
	return defaultProviderState.provider
}

// defaultDecisionResolver is TS resolveDecisionModel's
// `(globalThis.AI_SDK_DEFAULT_PROVIDER ?? gateway)` with
// `provider.decisionModel ?? provider.evaluationModel`: nil when no default
// provider is set (the AI Gateway is used), the TS NoSuchModelError when the
// default provider has no decision model factory.
func defaultDecisionResolver(modelID string) (provider.DecisionModelProvider, error) {
	switch p := configuredDefaultProvider().(type) {
	case nil:
		return nil, nil
	case provider.DecisionModelProvider:
		return p, nil
	case provider.EvaluationModelProvider: //nolint:staticcheck // deprecated compatibility path
		return provider.AdaptEvaluationModelProvider(p), nil //nolint:staticcheck // deprecated compatibility path
	}
	return nil, &providererrors.NoSuchModelError{
		ModelID:   modelID,
		ModelType: "decisionModel",
		Message:   "The default provider does not support decision models. Pass a decision model instance or configure AI_SDK_DEFAULT_PROVIDER with a decisionModel method.",
	}
}

package errors

import (
	"errors"
	"fmt"
)

// NoSuchModelError mirrors the TypeScript AI SDK's NoSuchModelError
// (AI_NoSuchModelError, @ai-sdk/provider errors/no-such-model-error.ts). It
// is returned when a provider was found but the requested model ID does not
// exist on it — as opposed to NoSuchProviderError, which is returned when
// the provider itself cannot be resolved.
type NoSuchModelError struct {
	// ModelID is the requested model identifier.
	ModelID string

	// ModelType identifies the kind of model requested, matching TS's
	// modelType union: "languageModel", "embeddingModel", "imageModel",
	// "transcriptionModel", "speechModel", "rerankingModel", "videoModel",
	// or "evaluationModel".
	ModelType string

	// Message is a custom error message. When empty, Error() falls back to
	// TS's default: `No such ${modelType}: ${modelId}`.
	Message string
}

// Error implements the error interface.
func (e *NoSuchModelError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("No such %s: %s", e.ModelType, e.ModelID)
}

// IsNoSuchModelError checks if an error is a NoSuchModelError.
func IsNoSuchModelError(err error) bool {
	var target *NoSuchModelError
	return errors.As(err, &target)
}

// NewNoSuchModelError creates a new NoSuchModelError with the default
// TS-style message (`No such ${modelType}: ${modelId}`).
func NewNoSuchModelError(modelID, modelType string) *NoSuchModelError {
	return &NoSuchModelError{ModelID: modelID, ModelType: modelType}
}

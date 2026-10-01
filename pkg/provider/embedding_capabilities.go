package provider

import "context"

// EmbeddingModelMaxInputBytesPerCall is an optional, experimental capability
// an EmbeddingModel can implement to expose the UTF-8 input byte budget of a
// single provider call. ai.EmbedMany splits requests so that each call stays
// within both MaxEmbeddingsPerCall and MaxInputBytesPerCall.
//
// A return value <= 0 means "no byte limit".
//
// Mirrors the TypeScript SDK's EXPERIMENTAL_EMBEDDING_MODEL_MAX_INPUT_BYTES_PER_CALL
// symbol capability, which intentionally lives outside the versioned
// embedding model specification.
type EmbeddingModelMaxInputBytesPerCall interface {
	MaxInputBytesPerCall() int
}

// EmbeddingProviderOptionsTransformInput describes one automatically batched
// embedding call. Values holds the full EmbedMany input; the batch covers
// Values[StartIndex:EndIndex].
type EmbeddingProviderOptionsTransformInput struct {
	ProviderOptions map[string]interface{}
	Values          []string
	StartIndex      int
	EndIndex        int
}

// EmbeddingModelProviderOptionsTransformer is an optional, experimental
// capability an EmbeddingModel can implement to adjust provider options for an
// automatically batched call (for example, slicing per-value multimodal content
// so it stays aligned with the values in each batch).
//
// ai.EmbedMany calls it once per batch, before the retry loop, so a failed
// batch is retried with the same options.
//
// Mirrors the TypeScript SDK's
// EXPERIMENTAL_EMBEDDING_MODEL_PROVIDER_OPTIONS_TRANSFORMER symbol capability.
type EmbeddingModelProviderOptionsTransformer interface {
	TransformEmbeddingProviderOptions(ctx context.Context, input EmbeddingProviderOptionsTransformInput) (map[string]interface{}, error)
}

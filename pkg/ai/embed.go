package ai

import (
	"context"
	"fmt"
	"math"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// EmbedOptions contains options for embedding generation
type EmbedOptions struct {
	// Model to use for embedding
	Model provider.EmbeddingModel

	// Input text to embed
	Input string

	// Telemetry configuration for observability
	ExperimentalTelemetry *TelemetrySettings
}

// EmbedResult contains the result of an embedding operation
type EmbedResult struct {
	// Embedding vector
	Embedding []float64

	// Usage information
	Usage types.EmbeddingUsage
}

// Embed generates an embedding for a single text input
func Embed(ctx context.Context, opts EmbedOptions) (*EmbedResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.Input == "" {
		return nil, fmt.Errorf("input is required")
	}

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && opts.ExperimentalTelemetry.IsEnabled {
		tracer := telemetry.GetTracer(opts.ExperimentalTelemetry)

		// Create top-level ai.embed span
		spanName := "ai.embed"
		if opts.ExperimentalTelemetry.FunctionID != "" {
			spanName = spanName + "." + opts.ExperimentalTelemetry.FunctionID
		}

		ctx, span = tracer.Start(ctx, spanName)
		defer span.End()

		// Add base telemetry attributes
		span.SetAttributes(
			attribute.String("ai.operationId", "ai.embed"),
			attribute.String("ai.model.provider", opts.Model.Provider()),
			attribute.String("ai.model.id", opts.Model.ModelID()),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata
		for key, value := range opts.ExperimentalTelemetry.Metadata {
			span.SetAttributes(attribute.KeyValue{
				Key:   attribute.Key("ai.telemetry.metadata." + key),
				Value: value,
			})
		}

		// Record input if enabled
		if opts.ExperimentalTelemetry.RecordInputs {
			span.SetAttributes(attribute.String("ai.value", opts.Input))
		}
	}

	// Call the model
	result, err := opts.Model.DoEmbed(ctx, opts.Input)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	embedResult := &EmbedResult{
		Embedding: result.Embedding,
		Usage:     result.Usage,
	}

	// Record telemetry output attributes
	if span != nil {
		// Record usage information
		span.SetAttributes(attribute.Int("ai.usage.tokens", embedResult.Usage.TotalTokens))
	}

	return embedResult, nil
}

// EmbedManyOptions contains options for batch embedding generation
type EmbedManyOptions struct {
	// Model to use for embedding
	Model provider.EmbeddingModel

	// Input texts to embed
	Inputs []string

	// Telemetry configuration for observability
	ExperimentalTelemetry *TelemetrySettings
}

// EmbedManyResult contains the result of a batch embedding operation
type EmbedManyResult struct {
	// Embeddings for each input
	Embeddings [][]float64

	// Usage information
	Usage types.EmbeddingUsage
}

// EmbedMany generates embeddings for multiple text inputs in a batch
func EmbedMany(ctx context.Context, opts EmbedManyOptions) (*EmbedManyResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if len(opts.Inputs) == 0 {
		return nil, fmt.Errorf("at least one input is required")
	}

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && opts.ExperimentalTelemetry.IsEnabled {
		tracer := telemetry.GetTracer(opts.ExperimentalTelemetry)

		// Create top-level ai.embedMany span
		spanName := "ai.embedMany"
		if opts.ExperimentalTelemetry.FunctionID != "" {
			spanName = spanName + "." + opts.ExperimentalTelemetry.FunctionID
		}

		ctx, span = tracer.Start(ctx, spanName)
		defer span.End()

		// Add base telemetry attributes
		span.SetAttributes(
			attribute.String("ai.operationId", "ai.embedMany"),
			attribute.String("ai.model.provider", opts.Model.Provider()),
			attribute.String("ai.model.id", opts.Model.ModelID()),
			attribute.Int("ai.values.count", len(opts.Inputs)),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata
		for key, value := range opts.ExperimentalTelemetry.Metadata {
			span.SetAttributes(attribute.KeyValue{
				Key:   attribute.Key("ai.telemetry.metadata." + key),
				Value: value,
			})
		}
	}

	// Call the model
	result, err := opts.Model.DoEmbedMany(ctx, opts.Inputs)
	if err != nil {
		return nil, fmt.Errorf("batch embedding failed: %w", err)
	}

	embedResult := &EmbedManyResult{
		Embeddings: result.Embeddings,
		Usage:      result.Usage,
	}

	// Record telemetry output attributes
	if span != nil {
		// Record usage information
		span.SetAttributes(attribute.Int("ai.usage.tokens", embedResult.Usage.TotalTokens))
	}

	return embedResult, nil
}

// CosineSimilarity calculates the cosine similarity between two embeddings
// Returns a value between -1 (opposite) and 1 (identical)
func CosineSimilarity(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions must match: %d != %d", len(a), len(b))
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	// Avoid division by zero
	if normA == 0 || normB == 0 {
		return 0, fmt.Errorf("cannot compute similarity for zero vector")
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)), nil
}

// EuclideanDistance calculates the Euclidean distance between two embeddings
// Returns a non-negative value where 0 means identical
func EuclideanDistance(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions must match: %d != %d", len(a), len(b))
	}

	var sum float64
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return math.Sqrt(sum), nil
}

// DotProduct calculates the dot product of two embeddings
func DotProduct(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions must match: %d != %d", len(a), len(b))
	}

	var product float64
	for i := range a {
		product += a[i] * b[i]
	}

	return product, nil
}

// Normalize normalizes an embedding to unit length
func Normalize(embedding []float64) []float64 {
	var norm float64
	for _, v := range embedding {
		norm += v * v
	}
	norm = math.Sqrt(norm)

	if norm == 0 {
		return embedding
	}

	normalized := make([]float64, len(embedding))
	for i, v := range embedding {
		normalized[i] = v / norm
	}

	return normalized
}

// FindMostSimilar finds the most similar embedding to a query from a list
// Returns the index and similarity score
func FindMostSimilar(query []float64, candidates [][]float64) (index int, similarity float64, err error) {
	if len(candidates) == 0 {
		return -1, 0, fmt.Errorf("candidates list is empty")
	}

	maxSimilarity := -2.0 // Below minimum possible similarity
	maxIndex := -1

	for i, candidate := range candidates {
		sim, err := CosineSimilarity(query, candidate)
		if err != nil {
			return -1, 0, err
		}

		if sim > maxSimilarity {
			maxSimilarity = sim
			maxIndex = i
		}
	}

	return maxIndex, maxSimilarity, nil
}

// RankBySimilarity ranks embeddings by their similarity to a query
// Returns indices sorted by similarity (most similar first)
func RankBySimilarity(query []float64, candidates [][]float64) ([]int, []float64, error) {
	if len(candidates) == 0 {
		return []int{}, []float64{}, nil
	}

	// Calculate similarities
	type result struct {
		index      int
		similarity float64
	}

	results := make([]result, len(candidates))
	for i, candidate := range candidates {
		sim, err := CosineSimilarity(query, candidate)
		if err != nil {
			return nil, nil, err
		}
		results[i] = result{index: i, similarity: sim}
	}

	// Sort by similarity (descending)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].similarity > results[i].similarity {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Extract indices and similarities
	indices := make([]int, len(results))
	similarities := make([]float64, len(results))
	for i, r := range results {
		indices[i] = r.index
		similarities[i] = r.similarity
	}

	return indices, similarities, nil
}

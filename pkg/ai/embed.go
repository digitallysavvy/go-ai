package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// newCallID generates a short random hex string for correlating start/finish events.
func newCallID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// EmbedOnStartEvent is emitted before calling the embedding model.
type EmbedOnStartEvent struct {
	// CallID is a unique identifier for this embedding call, correlates with EmbedOnFinishEvent.
	CallID string
	// OperationID is the canonical operation name (e.g. "ai.embed").
	OperationID string
	// Provider and ModelID identify the model.
	Provider string
	ModelID  string
	// Values are the input texts being embedded (single value for Embed,
	// multiple for EmbedMany).
	Values []string
	// MaxRetries is the configured retry limit (0 if not set).
	MaxRetries int
	// Ctx is the context passed to Embed / EmbedMany.
	Ctx context.Context
	// Headers are any extra HTTP headers forwarded to the model.
	Headers map[string]string
	// ProviderOptions holds provider-specific options keyed by provider name.
	// Example: map[string]interface{}{"openai": map[string]interface{}{"dimensions": 256}}
	ProviderOptions map[string]interface{}
	// TelemetryEnabled indicates whether telemetry is active for this call.
	TelemetryEnabled bool
	// IsEnabled mirrors TelemetryEnabled for TS SDK API parity.
	IsEnabled bool
	// RecordInputs indicates whether inputs are recorded in telemetry.
	RecordInputs bool
	// RecordOutputs indicates whether outputs are recorded in telemetry.
	RecordOutputs bool
	// FunctionID is the telemetry function identifier for grouping related operations.
	FunctionID string
	// Metadata holds additional telemetry key-value pairs.
	Metadata map[string]any
}

// EmbedOnFinishEvent is emitted after the embedding model returns.
type EmbedOnFinishEvent struct {
	// CallID matches the CallID in the corresponding EmbedOnStartEvent.
	CallID string
	// OperationID is the canonical operation name.
	OperationID string
	// Provider and ModelID identify the model.
	Provider string
	ModelID  string
	// Value echoes the input(s) that were embedded (single string for Embed, slice for EmbedMany).
	Value []string
	// Embeddings contains the resulting vectors (one per input value).
	Embeddings [][]float64
	// Usage reports token consumption for this call.
	Usage types.EmbeddingUsage
	// Warnings are any non-fatal warnings emitted by the provider.
	Warnings []types.Warning
	// ProviderMetadata holds arbitrary provider-specific JSON metadata.
	ProviderMetadata json.RawMessage
	// Responses holds the HTTP response metadata from the provider.
	// Length 1 for Embed; one entry per batch request for EmbedMany.
	Responses []types.EmbeddingResponse
	// IsEnabled indicates whether telemetry was active for this call.
	IsEnabled bool
	// RecordInputs indicates whether inputs are recorded in telemetry.
	RecordInputs bool
	// RecordOutputs indicates whether outputs are recorded in telemetry.
	RecordOutputs bool
	// FunctionID is the telemetry function identifier.
	FunctionID string
	// Metadata holds additional telemetry key-value pairs.
	Metadata map[string]any
}

// EmbedOptions contains options for embedding generation
type EmbedOptions struct {
	// Model to use for embedding
	Model provider.EmbeddingModel

	// Input text to embed
	Input string

	// MaxRetries is the number of times to retry on transient failure (0 = no retries).
	MaxRetries int

	// Headers are additional HTTP headers forwarded to the model on each request.
	Headers map[string]string

	// ProviderOptions holds provider-specific options forwarded to the model.
	// Keyed by provider name, e.g. map[string]interface{}{"openai": map[string]interface{}{"dimensions": 256}}.
	ProviderOptions map[string]interface{}

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// ExperimentalOnStart is called before the embedding model is invoked.
	ExperimentalOnStart func(event EmbedOnStartEvent)

	// ExperimentalOnFinish is called after the embedding model returns.
	ExperimentalOnFinish func(event EmbedOnFinishEvent)
}

// EmbedResult contains the result of an embedding operation
type EmbedResult struct {
	// Embedding vector
	Embedding []float64

	// Usage information
	Usage types.EmbeddingUsage

	// Warnings are any non-fatal warnings emitted by the provider.
	Warnings []types.Warning
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
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && telemetry.Enabled(opts.ExperimentalTelemetry) {
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
			attribute.String("gen_ai.system", opts.Model.Provider()),
			attribute.String("gen_ai.request.model", opts.Model.ModelID()),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata

		// Record input if enabled
		if opts.ExperimentalTelemetry.RecordInputs {
			span.SetAttributes(attribute.String("ai.value", opts.Input))
		}
	}

	// Generate a unique call ID for correlating start/finish events.
	callID := newCallID()

	// Extract telemetry fields for callback population.
	telEnabled := opts.ExperimentalTelemetry != nil && telemetry.Enabled(opts.ExperimentalTelemetry)
	var telFuncID string
	var telRecordInputs, telRecordOutputs bool
	if opts.ExperimentalTelemetry != nil {
		telFuncID = opts.ExperimentalTelemetry.FunctionID
		telRecordInputs = opts.ExperimentalTelemetry.RecordInputs
		telRecordOutputs = opts.ExperimentalTelemetry.RecordOutputs
	}

	// Fire ExperimentalOnStart callback
	startEvent := EmbedOnStartEvent{
		CallID:           callID,
		OperationID:      "ai.embed",
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Values:           []string{opts.Input},
		MaxRetries:       opts.MaxRetries,
		Ctx:              ctx,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		TelemetryEnabled: telEnabled,
		IsEnabled:        telEnabled,
		RecordInputs:     telRecordInputs,
		RecordOutputs:    telRecordOutputs,
		FunctionID:       telFuncID,
	}
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnEmbedStart, startEvent)
	}
	if opts.ExperimentalOnStart != nil {
		opts.ExperimentalOnStart(startEvent)
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.embed",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       opts.ExperimentalTelemetry,
		Prompt:         telemetryInputValue(opts.ExperimentalTelemetry, opts.Input),
		RuntimeContext: map[string]interface{}{},
		ToolsContext:   map[string]interface{}{},
	})

	// Build provider-level options.
	embedModelOpts := &provider.EmbedModelOptions{
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
	}

	// Call the model
	embedCallID := newCallID()
	telemetry.FireOnEmbedStart(ctx, telemetry.EmbeddingModelCallStartEvent{
		Settings:      opts.ExperimentalTelemetry,
		CallID:        callID,
		EmbedCallID:   embedCallID,
		OperationID:   "ai.embed.doEmbed",
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Values:        []string{opts.Input},
	})
	result, err := opts.Model.DoEmbed(ctx, opts.Input, embedModelOpts)
	if err != nil {
		wrappedErr := fmt.Errorf("embedding failed: %w", err)
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: wrappedErr})
		return nil, wrappedErr
	}

	embedResult := &EmbedResult{
		Embedding: result.Embedding,
		Usage:     result.Usage,
		Warnings:  result.Warnings,
	}

	// Record telemetry output attributes
	if span != nil {
		// Record usage information
		span.SetAttributes(attribute.Int("ai.usage.tokens", embedResult.Usage.TotalTokens))
	}
	telemetry.FireOnEmbedFinish(ctx, telemetry.EmbeddingModelCallEndEvent{
		Settings:      opts.ExperimentalTelemetry,
		CallID:        callID,
		EmbedCallID:   embedCallID,
		OperationID:   "ai.embed.doEmbed",
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Values:        []string{opts.Input},
		Embeddings:    [][]float64{embedResult.Embedding},
		Usage:         embedResult.Usage,
	})

	// Fire ExperimentalOnFinish callback
	finishEvent := EmbedOnFinishEvent{
		CallID:        callID,
		OperationID:   "ai.embed",
		Provider:      opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Value:         []string{opts.Input},
		Embeddings:    [][]float64{embedResult.Embedding},
		Usage:         embedResult.Usage,
		Warnings:      embedResult.Warnings,
		Responses:     []types.EmbeddingResponse{result.Response},
		IsEnabled:     telEnabled,
		RecordInputs:  telRecordInputs,
		RecordOutputs: telRecordOutputs,
		FunctionID:    telFuncID,
	}
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnEmbedFinish, finishEvent)
	}
	if opts.ExperimentalOnFinish != nil {
		opts.ExperimentalOnFinish(finishEvent)
	}
	telemetry.FireOnFinish(ctx, telemetry.TelemetryFinishEvent{
		Settings:      opts.ExperimentalTelemetry,
		FinishReason:  string(types.FinishReasonStop),
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Text:          "",
		Usage:         telemetryUsageFromEmbeddingUsage(embedResult.Usage),
	})

	return embedResult, nil
}

// EmbedManyOptions contains options for batch embedding generation
type EmbedManyOptions struct {
	// Model to use for embedding
	Model provider.EmbeddingModel

	// Input texts to embed
	Inputs []string

	// MaxRetries is the number of times to retry on transient failure (0 = no retries).
	MaxRetries int

	// Headers are additional HTTP headers forwarded to the model on each request.
	Headers map[string]string

	// ProviderOptions holds provider-specific options forwarded to the model.
	// Keyed by provider name, e.g. map[string]interface{}{"openai": map[string]interface{}{"dimensions": 256}}.
	ProviderOptions map[string]interface{}

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// ExperimentalOnStart is called before the embedding model is invoked.
	ExperimentalOnStart func(event EmbedOnStartEvent)

	// ExperimentalOnFinish is called after the embedding model returns.
	ExperimentalOnFinish func(event EmbedOnFinishEvent)
}

// EmbedManyResult contains the result of a batch embedding operation
type EmbedManyResult struct {
	// Embeddings for each input
	Embeddings [][]float64

	// Usage information
	Usage types.EmbeddingUsage

	// Warnings are any non-fatal warnings emitted by the provider.
	Warnings []types.Warning
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
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && telemetry.Enabled(opts.ExperimentalTelemetry) {
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
			attribute.String("gen_ai.system", opts.Model.Provider()),
			attribute.String("gen_ai.request.model", opts.Model.ModelID()),
			attribute.Int("ai.values.count", len(opts.Inputs)),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata
	}

	// Generate a unique call ID for correlating start/finish events.
	callID := newCallID()

	// Extract telemetry fields for callback population.
	telEnabled := opts.ExperimentalTelemetry != nil && telemetry.Enabled(opts.ExperimentalTelemetry)
	var telFuncID string
	var telRecordInputs, telRecordOutputs bool
	if opts.ExperimentalTelemetry != nil {
		telFuncID = opts.ExperimentalTelemetry.FunctionID
		telRecordInputs = opts.ExperimentalTelemetry.RecordInputs
		telRecordOutputs = opts.ExperimentalTelemetry.RecordOutputs
	}

	// Fire ExperimentalOnStart callback
	startEvent := EmbedOnStartEvent{
		CallID:           callID,
		OperationID:      "ai.embedMany",
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Values:           opts.Inputs,
		MaxRetries:       opts.MaxRetries,
		Ctx:              ctx,
		Headers:          opts.Headers,
		ProviderOptions:  opts.ProviderOptions,
		TelemetryEnabled: telEnabled,
		IsEnabled:        telEnabled,
		RecordInputs:     telRecordInputs,
		RecordOutputs:    telRecordOutputs,
		FunctionID:       telFuncID,
	}
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnEmbedStart, startEvent)
	}
	if opts.ExperimentalOnStart != nil {
		opts.ExperimentalOnStart(startEvent)
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.embedMany",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       opts.ExperimentalTelemetry,
		Prompt:         telemetryInputValue(opts.ExperimentalTelemetry, opts.Inputs),
		ValueCount:     len(opts.Inputs),
		RuntimeContext: map[string]interface{}{},
		ToolsContext:   map[string]interface{}{},
	})

	// Build provider-level options.
	embedModelOpts := &provider.EmbedModelOptions{
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
	}

	// Call the model
	embedCallID := newCallID()
	telemetry.FireOnEmbedStart(ctx, telemetry.EmbeddingModelCallStartEvent{
		Settings:      opts.ExperimentalTelemetry,
		CallID:        callID,
		EmbedCallID:   embedCallID,
		OperationID:   "ai.embedMany.doEmbed",
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Values:        opts.Inputs,
	})
	result, err := opts.Model.DoEmbedMany(ctx, opts.Inputs, embedModelOpts)
	if err != nil {
		wrappedErr := fmt.Errorf("batch embedding failed: %w", err)
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: wrappedErr})
		return nil, wrappedErr
	}

	embedResult := &EmbedManyResult{
		Embeddings: result.Embeddings,
		Usage:      result.Usage,
		Warnings:   result.Warnings,
	}

	// Record telemetry output attributes
	if span != nil {
		// Record usage information
		span.SetAttributes(attribute.Int("ai.usage.tokens", embedResult.Usage.TotalTokens))
	}
	telemetry.FireOnEmbedFinish(ctx, telemetry.EmbeddingModelCallEndEvent{
		Settings:      opts.ExperimentalTelemetry,
		CallID:        callID,
		EmbedCallID:   embedCallID,
		OperationID:   "ai.embedMany.doEmbed",
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Values:        opts.Inputs,
		Embeddings:    embedResult.Embeddings,
		Usage:         embedResult.Usage,
	})

	// Fire ExperimentalOnFinish callback
	finishEvent := EmbedOnFinishEvent{
		CallID:        callID,
		OperationID:   "ai.embedMany",
		Provider:      opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Value:         opts.Inputs,
		Embeddings:    embedResult.Embeddings,
		Usage:         embedResult.Usage,
		Warnings:      embedResult.Warnings,
		Responses:     result.Responses,
		IsEnabled:     telEnabled,
		RecordInputs:  telRecordInputs,
		RecordOutputs: telRecordOutputs,
		FunctionID:    telFuncID,
	}
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnEmbedFinish, finishEvent)
	}
	if opts.ExperimentalOnFinish != nil {
		opts.ExperimentalOnFinish(finishEvent)
	}
	telemetry.FireOnFinish(ctx, telemetry.TelemetryFinishEvent{
		Settings:      opts.ExperimentalTelemetry,
		FinishReason:  string(types.FinishReasonStop),
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Text:          "",
		Usage:         telemetryUsageFromEmbeddingUsage(embedResult.Usage),
	})

	return embedResult, nil
}

func telemetryUsageFromEmbeddingUsage(usage types.EmbeddingUsage) telemetry.TelemetryUsage {
	total := int64(usage.TotalTokens)
	return telemetry.TelemetryUsage{TotalTokens: &total}
}

func telemetryInputValue(settings *TelemetrySettings, value interface{}) string {
	if settings != nil && !settings.RecordInputs {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(b)
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

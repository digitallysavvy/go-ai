package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// RerankOnStartEvent is emitted before calling the reranking model.
type RerankOnStartEvent struct {
	// CallID is a unique identifier for this reranking call, correlates with RerankOnFinishEvent.
	CallID string
	// OperationID is the canonical operation name ("ai.rerank").
	OperationID string
	// Provider and ModelID identify the model.
	Provider string
	ModelID  string
	// Query is the query being used to rerank documents.
	Query string
	// Documents are the documents being reranked.
	Documents interface{}
	// TopN is the requested number of top results (nil means return all).
	TopN *int
	// MaxRetries is the configured retry limit (0 if not set).
	MaxRetries int
	// Headers are any extra HTTP headers forwarded to the model.
	Headers map[string]string
	// ProviderOptions holds provider-specific options keyed by provider name.
	// Example: map[string]interface{}{"cohere": map[string]interface{}{"returnDocuments": true}}
	ProviderOptions map[string]interface{}
	// IsEnabled indicates whether telemetry is active for this call.
	IsEnabled bool
	// RecordInputs indicates whether inputs are recorded in telemetry.
	RecordInputs bool
	// RecordOutputs indicates whether outputs are recorded in telemetry.
	RecordOutputs bool
	// FunctionID is the telemetry function identifier.
	FunctionID string
}

// RerankOnFinishEvent is emitted after the reranking model returns.
type RerankOnFinishEvent struct {
	// CallID matches the CallID in the corresponding RerankOnStartEvent.
	CallID string
	// OperationID is the canonical operation name ("ai.rerank").
	OperationID string
	// Provider and ModelID identify the model.
	Provider string
	ModelID  string
	// Documents are the documents that were reranked.
	Documents interface{}
	// Query is the query that documents were reranked against.
	Query string
	// Ranking is the reranked results sorted by relevance score (descending).
	Ranking []RerankItem
	// Warnings are any non-fatal warnings emitted by the provider.
	Warnings []types.Warning
	// Response holds structured response metadata (id, timestamp, modelId, headers).
	Response types.RerankResponse
	// ProviderMetadata holds arbitrary provider-specific JSON metadata.
	ProviderMetadata json.RawMessage
	// Result is the full reranking result (for backward compatibility).
	Result *RerankResult
	// IsEnabled indicates whether telemetry was active for this call.
	IsEnabled bool
	// RecordInputs indicates whether inputs are recorded in telemetry.
	RecordInputs bool
	// RecordOutputs indicates whether outputs are recorded in telemetry.
	RecordOutputs bool
	// FunctionID is the telemetry function identifier.
	FunctionID string
}

// RerankOptions contains options for document reranking
type RerankOptions struct {
	// Model to use for reranking
	Model provider.RerankingModel

	// Documents to rerank (can be []string or []map[string]interface{})
	Documents interface{}

	// Query to rerank documents against
	Query string

	// TopN specifies the number of top documents to return
	// If nil or 0, all documents are returned
	TopN *int

	// MaxRetries is the number of times to retry on transient failure (0 = no retries).
	MaxRetries int

	// Headers are additional HTTP headers forwarded to the model on each request.
	Headers map[string]string

	// ProviderOptions holds provider-specific options forwarded to the model.
	// Keyed by provider name, e.g. map[string]interface{}{"cohere": map[string]interface{}{"returnDocuments": true}}.
	ProviderOptions map[string]interface{}

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// Callback called when reranking finishes
	OnFinish func(result *RerankResult)

	// ExperimentalOnStart is called before the reranking model is invoked.
	ExperimentalOnStart func(event RerankOnStartEvent)

	// ExperimentalOnFinish is called after the reranking model returns.
	ExperimentalOnFinish func(event RerankOnFinishEvent)
}

// RerankResult contains the result of a reranking operation
type RerankResult struct {
	// Original documents in their original order
	OriginalDocuments interface{}

	// Ranking contains indices and scores in relevance order
	Ranking []RerankItem

	// Reranked documents in relevance order
	RerankedDocuments interface{}

	// Response metadata
	Response types.RerankResponse

	// Warnings are any non-fatal warnings emitted by the provider.
	Warnings []types.Warning

	// Provider-specific metadata
	ProviderMetadata interface{}
}

// RerankItem represents a single reranked document with its score
type RerankItem struct {
	// Index of the document in the original list
	OriginalIndex int

	// Relevance score (higher is more relevant)
	Score float64

	// The actual document (if documents were provided)
	Document interface{}
}

// Rerank reranks documents according to their relevance to a query
func Rerank(ctx context.Context, opts RerankOptions) (*RerankResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.Documents == nil {
		return nil, fmt.Errorf("documents are required")
	}
	if opts.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)

	// Validate documents type
	var documentsSlice []interface{}
	switch docs := opts.Documents.(type) {
	case []string:
		documentsSlice = make([]interface{}, len(docs))
		for i, d := range docs {
			documentsSlice[i] = d
		}
	case []map[string]interface{}:
		documentsSlice = make([]interface{}, len(docs))
		for i, d := range docs {
			documentsSlice[i] = d
		}
	case []interface{}:
		documentsSlice = docs
	default:
		return nil, fmt.Errorf("documents must be []string, []map[string]interface{}, or []interface{}")
	}

	if len(documentsSlice) == 0 {
		// Return empty result for empty documents
		return &RerankResult{
			OriginalDocuments: opts.Documents,
			Ranking:           []RerankItem{},
			RerankedDocuments: opts.Documents,
			Response: types.RerankResponse{
				ModelID:   opts.Model.ModelID(),
				Timestamp: timeNow(),
			},
		}, nil
	}

	// Generate a unique call ID for correlating start/finish events.
	callID := newCallID()

	// Extract telemetry fields for callback population.
	telEnabled := false
	var telFuncID string
	var telRecordInputs, telRecordOutputs bool
	if opts.ExperimentalTelemetry != nil {
		telEnabled = telemetry.Enabled(opts.ExperimentalTelemetry)
		telFuncID = opts.ExperimentalTelemetry.FunctionID
		telRecordInputs = opts.ExperimentalTelemetry.RecordInputs
		telRecordOutputs = opts.ExperimentalTelemetry.RecordOutputs
	}

	// Fire ExperimentalOnStart callback
	startEvent := RerankOnStartEvent{
		CallID:          callID,
		OperationID:     "ai.rerank",
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		Query:           opts.Query,
		Documents:       opts.Documents,
		TopN:            opts.TopN,
		MaxRetries:      opts.MaxRetries,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
		IsEnabled:       telEnabled,
		RecordInputs:    telRecordInputs,
		RecordOutputs:   telRecordOutputs,
		FunctionID:      telFuncID,
	}
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnRerankStart, startEvent)
	}
	if opts.ExperimentalOnStart != nil {
		opts.ExperimentalOnStart(startEvent)
	}

	// Build rerank options — thread caller-supplied headers and provider options to the provider.
	rerankOpts := &provider.RerankOptions{
		Documents:       opts.Documents,
		Query:           opts.Query,
		TopN:            opts.TopN,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	}

	// Call the model
	modelResult, err := opts.Model.DoRerank(ctx, rerankOpts)
	if err != nil {
		return nil, fmt.Errorf("reranking failed: %w", err)
	}

	// Build result
	ranking := make([]RerankItem, len(modelResult.Ranking))
	rerankedDocs := make([]interface{}, len(modelResult.Ranking))

	for i, item := range modelResult.Ranking {
		ranking[i] = RerankItem{
			OriginalIndex: item.Index,
			Score:         item.RelevanceScore,
			Document:      documentsSlice[item.Index],
		}
		rerankedDocs[i] = documentsSlice[item.Index]
	}

	result := &RerankResult{
		OriginalDocuments: opts.Documents,
		Ranking:           ranking,
		RerankedDocuments: rerankedDocs,
		Response:          modelResult.Response,
		Warnings:          modelResult.Warnings,
		ProviderMetadata:  modelResult.ProviderMetadata,
	}

	// Call finish callback
	if opts.OnFinish != nil {
		opts.OnFinish(result)
	}

	// Fire ExperimentalOnFinish callback
	var providerMeta json.RawMessage
	if result.ProviderMetadata != nil {
		if b, merr := json.Marshal(result.ProviderMetadata); merr == nil {
			providerMeta = b
		}
	}
	finishEvent := RerankOnFinishEvent{
		CallID:           callID,
		OperationID:      "ai.rerank",
		Provider:         opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Documents:        opts.Documents,
		Query:            opts.Query,
		Ranking:          result.Ranking,
		Warnings:         result.Warnings,
		Response:         result.Response,
		ProviderMetadata: providerMeta,
		Result:           result,
		IsEnabled:        telEnabled,
		RecordInputs:     telRecordInputs,
		RecordOutputs:    telRecordOutputs,
		FunctionID:       telFuncID,
	}
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnRerankFinish, finishEvent)
	}
	if opts.ExperimentalOnFinish != nil {
		opts.ExperimentalOnFinish(finishEvent)
	}

	return result, nil
}

// Helper to get current time (makes testing easier)
var timeNow = func() time.Time {
	return time.Now()
}

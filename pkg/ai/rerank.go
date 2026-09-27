package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
	// MaxRetries is the configured retry limit (resolved; defaults to 2).
	MaxRetries int
	// RuntimeContext is the user-defined runtime context passed via the
	// options (unfiltered; treat as immutable).
	RuntimeContext interface{}
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
	// RuntimeContext is the user-defined runtime context passed via the
	// options (unfiltered; treat as immutable).
	RuntimeContext interface{}
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

// RerankStartEvent is the canonical name for RerankOnStartEvent (TS parity,
// 29d8cf4 event renames).
type RerankStartEvent = RerankOnStartEvent

// RerankEndEvent is the canonical name for RerankOnFinishEvent (TS parity,
// 29d8cf4 event renames).
type RerankEndEvent = RerankOnFinishEvent

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

	// MaxRetries is the number of times to retry a model call on a retryable
	// provider failure (HTTP 408/409/429/5xx), with exponential backoff that
	// respects retry-after headers. nil means unset and defaults to 2 (TS
	// default); 0 disables retries; negative values are rejected. This
	// mirrors the *int convention used by GenerateTextOptions.MaxRetries.
	MaxRetries *int

	// RuntimeContext is user-defined context passed to the start/end callbacks
	// unchanged. Treat it as immutable.
	RuntimeContext interface{}

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

	// OnStart is called before the reranking model is invoked.
	OnStart func(event RerankOnStartEvent)

	// ExperimentalOnStart is a deprecated alias for OnStart.
	//
	// Deprecated: use OnStart.
	ExperimentalOnStart func(event RerankOnStartEvent)

	// OnEnd is called after the reranking model returns.
	OnEnd func(event RerankOnFinishEvent)

	// ExperimentalOnEnd is a deprecated alias for OnEnd.
	//
	// Deprecated: use OnEnd.
	ExperimentalOnEnd func(event RerankOnFinishEvent)

	// ExperimentalOnFinish is a deprecated alias for OnEnd.
	//
	// Deprecated: use OnEnd.
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

	if len(documentsSlice) == 0 {
		// TS rerank(): documents.length === 0 short-circuits before
		// prepareRetries, but still fires onStart/onEnd (rerank.test.ts
		// "should fire callbacks for empty documents").
		emptyStartEvent := RerankOnStartEvent{
			CallID:          callID,
			OperationID:     "ai.rerank",
			Provider:        opts.Model.Provider(),
			ModelID:         opts.Model.ModelID(),
			Query:           opts.Query,
			Documents:       opts.Documents,
			TopN:            opts.TopN,
			MaxRetries:      preparedMaxRetries(opts.MaxRetries),
			RuntimeContext:  opts.RuntimeContext,
			Headers:         opts.Headers,
			ProviderOptions: opts.ProviderOptions,
			IsEnabled:       telEnabled,
			RecordInputs:    telRecordInputs,
			RecordOutputs:   telRecordOutputs,
			FunctionID:      telFuncID,
		}
		if telemetry.Enabled(opts.ExperimentalTelemetry) {
			telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnRerankStart, emptyStartEvent)
		}
		if opts.OnStart != nil {
			opts.OnStart(emptyStartEvent)
		} else if opts.ExperimentalOnStart != nil {
			opts.ExperimentalOnStart(emptyStartEvent)
		}

		emptyResponse := types.RerankResponse{
			ModelID:   opts.Model.ModelID(),
			Timestamp: timeNow(),
		}
		emptyResult := &RerankResult{
			OriginalDocuments: opts.Documents,
			Ranking:           []RerankItem{},
			RerankedDocuments: opts.Documents,
			Response:          emptyResponse,
		}
		if opts.OnFinish != nil {
			opts.OnFinish(emptyResult)
		}
		emptyFinishEvent := RerankOnFinishEvent{
			CallID:         callID,
			OperationID:    "ai.rerank",
			Provider:       opts.Model.Provider(),
			ModelID:        opts.Model.ModelID(),
			Documents:      opts.Documents,
			Query:          opts.Query,
			RuntimeContext: opts.RuntimeContext,
			Ranking:        []RerankItem{},
			Warnings:       []types.Warning{},
			Response:       emptyResponse,
			Result:         emptyResult,
			IsEnabled:      telEnabled,
			RecordInputs:   telRecordInputs,
			RecordOutputs:  telRecordOutputs,
			FunctionID:     telFuncID,
		}
		if telemetry.Enabled(opts.ExperimentalTelemetry) {
			telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnRerankEnd, emptyFinishEvent)
		}
		if opts.OnEnd != nil {
			opts.OnEnd(emptyFinishEvent)
		} else {
			if opts.ExperimentalOnEnd != nil {
				opts.ExperimentalOnEnd(emptyFinishEvent)
			}
			if opts.ExperimentalOnFinish != nil {
				opts.ExperimentalOnFinish(emptyFinishEvent)
			}
		}
		return emptyResult, nil
	}

	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	resolvedMaxRetries := preparedMaxRetries(opts.MaxRetries)

	// Fire ExperimentalOnStart callback
	startEvent := RerankOnStartEvent{
		CallID:          callID,
		OperationID:     "ai.rerank",
		Provider:        opts.Model.Provider(),
		ModelID:         opts.Model.ModelID(),
		Query:           opts.Query,
		Documents:       opts.Documents,
		TopN:            opts.TopN,
		MaxRetries:      resolvedMaxRetries,
		RuntimeContext:  opts.RuntimeContext,
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
	if opts.OnStart != nil {
		opts.OnStart(startEvent)
	} else if opts.ExperimentalOnStart != nil {
		opts.ExperimentalOnStart(startEvent)
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.rerank",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       opts.ExperimentalTelemetry,
		Documents:      opts.Documents,
		Headers:        opts.Headers,
		MaxRetries:     &resolvedMaxRetries,
		RuntimeContext: telemetryRuntimeContext(opts.ExperimentalTelemetry, opts.RuntimeContext),
		ToolsContext:   map[string]interface{}{},
	})

	// Build rerank options — thread caller-supplied headers and provider options to the provider.
	rerankOpts := &provider.RerankOptions{
		Documents:       opts.Documents,
		Query:           opts.Query,
		TopN:            opts.TopN,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	}

	// Call the model, retried per withEmbedRetry (TS prepareRetries policy):
	// exponential backoff from 2s, respecting retry-after headers, retrying
	// only retryable provider errors. The per-attempt doRerank telemetry
	// fires inside the retry loop, as in TS rerank().
	documentsType := rerankDocumentsType(opts.Documents)
	var modelResult *types.RerankResult
	err := withEmbedRetry(ctx, resolvedMaxRetries, func(attemptCtx context.Context) error {
		telemetry.FireOnRerankStart(attemptCtx, telemetry.RerankingModelCallStartEvent{
			Settings:      opts.ExperimentalTelemetry,
			CallID:        callID,
			OperationID:   "ai.rerank.doRerank",
			ModelProvider: opts.Model.Provider(),
			ModelID:       opts.Model.ModelID(),
			Documents:     opts.Documents,
			DocumentsType: documentsType,
			Query:         opts.Query,
			TopN:          opts.TopN,
		})
		res, callErr := opts.Model.DoRerank(attemptCtx, rerankOpts)
		if callErr != nil {
			return callErr
		}
		telemetry.FireOnRerankEnd(attemptCtx, telemetry.RerankingModelCallEndEvent{
			Settings:      opts.ExperimentalTelemetry,
			CallID:        callID,
			OperationID:   "ai.rerank.doRerank",
			ModelProvider: opts.Model.Provider(),
			ModelID:       opts.Model.ModelID(),
			DocumentsType: documentsType,
			Ranking:       res.Ranking,
		})
		modelResult = res
		return nil
	})
	if err != nil {
		wrappedErr := fmt.Errorf("reranking failed: %w", err)
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: wrappedErr})
		return nil, wrappedErr
	}

	// TS rerank(): validateRankingIndices runs after the retry resolves and
	// before logWarnings/onEnd — an invalid index fails the call without
	// retrying and without firing onEnd (rerank.test.ts "should reject
	// invalid provider ranking index").
	if err := validateRankingIndices(modelResult.Ranking, documentsSlice); err != nil {
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: opts.ExperimentalTelemetry, Error: err})
		return nil, err
	}

	logModelWarnings(modelResult.Warnings, opts.Model.Provider(), opts.Model.ModelID())

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
		Warnings:          warningsOrEmpty(modelResult.Warnings),
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
		RuntimeContext:   opts.RuntimeContext,
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
		telemetry.PublishDiagnostic(ctx, telemetry.DiagnosticEventOnRerankEnd, finishEvent)
	}
	if opts.OnEnd != nil {
		opts.OnEnd(finishEvent)
	} else {
		if opts.ExperimentalOnEnd != nil {
			opts.ExperimentalOnEnd(finishEvent)
		}
		if opts.ExperimentalOnFinish != nil {
			opts.ExperimentalOnFinish(finishEvent)
		}
	}
	telemetry.FireOnFinish(ctx, telemetry.TelemetryFinishEvent{
		OperationType: "ai.rerank",
		Settings:      opts.ExperimentalTelemetry,
		FinishReason:  string(types.FinishReasonStop),
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Usage:         telemetry.TelemetryUsage{},
	})

	return result, nil
}

// validateRankingIndices rejects a ranking that references an out-of-range
// document index (TS rerank.ts validateRankingIndices). It must run before
// any code indexes documentsSlice[item.Index], which would otherwise panic
// on a bad index.
func validateRankingIndices(ranking []types.RerankItem, documents []interface{}) error {
	for _, item := range ranking {
		if item.Index < 0 || item.Index >= len(documents) {
			return providererrors.NewInvalidResponseDataError(ranking,
				fmt.Sprintf("Invalid ranking index %d. Expected an integer between 0 and %d.", item.Index, len(documents)-1))
		}
	}
	return nil
}

func rerankDocumentsType(documents interface{}) string {
	switch documents.(type) {
	case []string:
		return "text"
	default:
		return "object"
	}
}

// Helper to get current time (makes testing easier)
var timeNow = func() time.Time {
	return time.Now()
}

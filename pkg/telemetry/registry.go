package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ---------------------------------------------------------------------------
// Telemetry event types
// ---------------------------------------------------------------------------

// TelemetryStartEvent is passed to TelemetryIntegration.OnStart.
type TelemetryStartEvent struct {
	// OperationType is the canonical AI operation name, e.g. "ai.generateText".
	OperationType string
	ModelProvider string
	ModelID       string
	// Settings holds the caller-supplied telemetry configuration.
	// nil means telemetry was not configured for this call.
	Settings *Settings
	// Prompt, System, and Messages are only populated when
	// Settings.RecordInputs is true.
	Prompt string
	System string
	// Messages carries the caller-supplied message list (generateText/
	// streamText/generateObject/streamObject), used to build
	// gen_ai.input.messages on the root span (fc15550, 12cfe40).
	Messages []types.Message
	// ValueCount is populated for batch value operations such as embedMany.
	ValueCount int
	// Headers carries caller-supplied request headers, emitted as
	// ai.request.headers.* when OpenTelemetryOptions.Headers is set (18651f6).
	Headers map[string]string
	// Schema, SchemaName, and SchemaDescription are populated for
	// generateObject/streamObject and emitted as ai.schema* when
	// OpenTelemetryOptions.Schema is set (18651f6).
	Schema            map[string]interface{}
	SchemaName        string
	SchemaDescription string
	// RuntimeContext and ToolsContext contain only keys explicitly included by
	// Settings.IncludeRuntimeContext and Settings.IncludeToolsContext.
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}

	// MaxOutputTokens, Temperature, TopP, TopK, PresencePenalty,
	// FrequencyPenalty, StopSequences, and Seed are call-level generation
	// settings, emitted by LegacyOpenTelemetry as ai.settings.<key> on the
	// root span, mirroring the TS SDK's per-operation "settings" object built
	// before getBaseTelemetryAttributes (otel/src/legacy-open-telemetry.ts
	// onGenerateStart/onObjectOperationStart). StopSequences only applies to
	// generateText/streamText — TS's onObjectOperationStart settings object
	// omits it for generateObject/streamObject.
	MaxOutputTokens  *int
	Temperature      *float64
	TopP             *float64
	TopK             *int
	PresencePenalty  *float64
	FrequencyPenalty *float64
	StopSequences    []string
	Seed             *int
	// MaxRetries is emitted as ai.settings.maxRetries. TS includes it in
	// every operation's settings object (generateText, generateObject,
	// embed, embedMany, rerank all set it).
	MaxRetries *int
	// SettingsOutput carries generateObject/streamObject's output mode
	// ("object"/"array"/"enum"/"no-schema" etc.), emitted directly as
	// ai.settings.output. TS sets this outside the settings object
	// (`'ai.settings.output': event.output`), unconditionally rather than as
	// part of getBaseTelemetryAttributes.
	SettingsOutput string
	// Values holds the embedMany input strings, JSON-encoded per element and
	// emitted as ai.values (TS: event.values.map(v => JSON.stringify(v))).
	// For ai.embed (a single value), Prompt already carries the raw input
	// and LegacyOpenTelemetry JSON-encodes it into ai.value instead.
	Values []string
	// Documents holds the rerank input (typically []string or
	// []map[string]interface{}), JSON-encoded per element and emitted as
	// ai.documents (TS: event.documents.map(d => JSON.stringify(d))).
	Documents interface{}
}

// TelemetryStepStartEvent is passed to TelemetryIntegration.OnStepStart.
type TelemetryStepStartEvent struct {
	Settings *Settings
	// OperationType is the canonical AI operation name, e.g. "ai.generateText".
	// Used to name the per-step OTel child span.
	OperationType string
	StepNumber    int
	ModelProvider string
	ModelID       string
	// ToolChoice is the effective tool choice for this step, emitted as
	// ai.prompt.toolChoice when OpenTelemetryOptions.ToolChoice is set
	// (152c67c, 18651f6). The zero value (Type == "") means unset.
	ToolChoice     types.ToolChoice
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
	// PromptMessages carries the exact prompt sent to the provider for this
	// step, emitted as ai.prompt.messages (input-gated, JSON-encoded) by
	// LegacyOpenTelemetry.OnStepStart, mirroring TS's onStepStart
	// (legacy-open-telemetry.ts: `'ai.prompt.messages': {input: () =>
	// stringifyForTelemetry(event.promptMessages)}`). Go's types.Message list
	// stands in for TS's LanguageModelV4Prompt (which folds the system
	// message into the message array); this is an accepted Go-runtime
	// divergence, same as legacyPromptJSON elsewhere in this file.
	PromptMessages []types.Message
	// StepTools carries the tool definitions available for this step,
	// emitted as ai.prompt.tools (input-gated, one JSON string per tool)
	// mirroring TS's `'ai.prompt.tools': {input: () =>
	// event.stepTools?.map(tool => JSON.stringify(tool))}`.
	StepTools []types.Tool
}

// LanguageModelCallStartEvent is emitted immediately before a provider model call.
type LanguageModelCallStartEvent struct {
	Settings      *Settings
	CallID        string
	ModelProvider string
	ModelID       string
	Prompt        interface{}
	Tools         interface{}

	// System carries the effective system/instructions text for this call,
	// used for gen_ai.system_instructions in the GenAI integration.
	System string

	// Request settings, used for gen_ai.request.* in the GenAI integration.
	Temperature      *float64
	MaxOutputTokens  *int
	TopP             *float64
	TopK             *int
	PresencePenalty  *float64
	FrequencyPenalty *float64
	StopSequences    []string
	Seed             *int
}

// LanguageModelCallEndEvent is emitted after a provider model call returns and
// before client-side tool execution begins.
type LanguageModelCallEndEvent struct {
	Settings      *Settings
	CallID        string
	ModelProvider string
	ModelID       string
	FinishReason  string
	Usage         TelemetryUsage
	Content       interface{}
	ResponseID    string
	Performance   LanguageModelCallPerformance
	// ProviderMetadata holds provider-specific response metadata, emitted as
	// ai.response.providerMetadata when OpenTelemetryOptions.ProviderMetadata
	// is set (18651f6).
	ProviderMetadata map[string]interface{}
}

// LanguageModelCallPerformance contains timing statistics for provider model work.
type LanguageModelCallPerformance struct {
	ResponseTimeMs                 int64                         `json:"responseTimeMs"`
	EffectiveOutputTokensPerSecond float64                       `json:"effectiveOutputTokensPerSecond"`
	OutputTokensPerSecond          *float64                      `json:"outputTokensPerSecond,omitempty"`
	InputTokensPerSecond           *float64                      `json:"inputTokensPerSecond,omitempty"`
	EffectiveTotalTokensPerSecond  float64                       `json:"effectiveTotalTokensPerSecond"`
	TimeToFirstOutputMs            *int64                        `json:"timeToFirstOutputMs,omitempty"`
	TimeBetweenOutputChunksMs      *types.OutputChunkTimingStats `json:"timeBetweenOutputChunksMs,omitempty"`
}

// EmbeddingModelCallStartEvent is emitted immediately before an embedding model call.
type EmbeddingModelCallStartEvent struct {
	Settings      *Settings
	CallID        string
	EmbedCallID   string
	OperationID   string
	ModelProvider string
	ModelID       string
	Values        []string
}

// EmbeddingModelCallEndEvent is emitted after an embedding model call completes.
type EmbeddingModelCallEndEvent struct {
	Settings      *Settings
	CallID        string
	EmbedCallID   string
	OperationID   string
	ModelProvider string
	ModelID       string
	Values        []string
	Embeddings    [][]float64
	Usage         types.EmbeddingUsage
}

// RerankingModelCallStartEvent is emitted immediately before a reranking model call.
type RerankingModelCallStartEvent struct {
	Settings      *Settings
	CallID        string
	OperationID   string
	ModelProvider string
	ModelID       string
	Documents     interface{}
	DocumentsType string
	Query         string
	TopN          *int
}

// RerankingModelCallEndEvent is emitted after a reranking model call completes.
type RerankingModelCallEndEvent struct {
	Settings      *Settings
	CallID        string
	OperationID   string
	ModelProvider string
	ModelID       string
	DocumentsType string
	Ranking       []types.RerankItem
}

// TelemetryToolCallStartEvent is passed to TelemetryIntegration.OnToolExecutionStart.
type TelemetryToolCallStartEvent struct {
	Settings    *Settings
	ToolCallID  string
	ToolName    string
	Args        map[string]interface{}
	ToolContext map[string]interface{}
}

// TelemetryToolCallFinishEvent is passed to TelemetryIntegration.OnToolExecutionEnd.
type TelemetryToolCallFinishEvent struct {
	Settings    *Settings
	ToolCallID  string
	ToolName    string
	Args        map[string]interface{}
	Result      interface{}
	Error       error
	DurationMs  int64
	ToolContext map[string]interface{}
}

// TelemetryChunkEvent is passed to TelemetryIntegration.OnChunk (streaming only).
type TelemetryChunkEvent struct {
	Settings *Settings
	// ChunkType mirrors provider.ChunkType values: "text", "tool-call", "tool-result", etc.
	ChunkType string
	// Text is populated for text-type chunks.
	Text string
}

// TelemetryStepFinishEvent is the deprecated name for step-end telemetry.
//
// Deprecated: use TelemetryStepEndEvent.
type TelemetryStepFinishEvent struct {
	// OperationType is the canonical AI operation name for the call this step
	// belongs to (e.g. "ai.generateText", "ai.generateObject"). Legacy
	// integrations use it to pick the TS-equivalent per-operation step-end
	// attribute shape: generateText/streamText's onStepEnd (text/reasoning/
	// toolCalls/files + detailed usage) vs generateObject/streamObject's
	// deprecated-but-still-dispatched onObjectStepEnd (ai.response.object +
	// a smaller usage set, legacy-open-telemetry.ts).
	OperationType string
	StepNumber    int
	FinishReason  string
	Usage         TelemetryUsage

	// Text is the generated text for this step.
	// Integrations should check Settings.RecordOutputs before recording.
	Text string

	// Reasoning is the joined reasoning/thinking text for this step.
	// Integrations should check Settings.RecordOutputs before recording.
	Reasoning string

	// ToolCalls made by the model in this step.
	// Integrations should check Settings.RecordOutputs before recording.
	ToolCalls []types.ToolCall

	// Files holds model-generated output files for this step.
	// Integrations should check Settings.RecordOutputs before recording.
	Files []types.GeneratedFileContent

	// ProviderMetadata holds provider-specific response metadata for this step.
	ProviderMetadata map[string]interface{}

	// ResponseID is the provider-assigned response identifier for this step.
	ResponseID string

	// ResponseModelID is the model ID reported in the provider response.
	ResponseModelID string

	// ResponseTimestamp is when the provider response was received.
	ResponseTimestamp time.Time

	// Performance carries step timing (TimeToFirstOutputMs, ResponseTimeMs,
	// EffectiveOutputTokensPerSecond), mirroring TS's event.performance.
	// LegacyOpenTelemetry.OnStepEnd uses it for the ai.response.msToFirstChunk/
	// msToFinish/avgOutputTokensPerSecond attributes and the
	// ai.stream.firstChunk/ai.stream.finish span events (ai.streamText only),
	// and for the ai.stream.msToFirstChunk attribute/event on the
	// generateObject/streamObject onObjectStepEnd shape (TimeToFirstOutputMs
	// only; always nil for non-streaming generateObject, matching TS's
	// `msToFirstChunk: undefined`).
	Performance LanguageModelCallPerformance

	// Settings holds the caller-supplied telemetry configuration.
	Settings       *Settings
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
}

// TelemetryStepEndEvent is the canonical name for TelemetryStepFinishEvent.
type TelemetryStepEndEvent = TelemetryStepFinishEvent

type deprecatedStepFinishHandler interface {
	OnStepFinish(ctx context.Context, e TelemetryStepFinishEvent)
}

// TelemetryFinishEvent is passed to TelemetryIntegration.OnFinish.
type TelemetryFinishEvent struct {
	// OperationType is the canonical AI operation name (e.g. "ai.generateText",
	// "ai.generateObject", "ai.embed", "ai.embedMany", "ai.rerank"). Legacy
	// integrations dispatch on it to reproduce TS's per-operation OnEnd shape
	// (onGenerateEnd / onObjectOperationEnd / onEmbedOperationEnd /
	// onRerankOperationEnd in legacy-open-telemetry.ts), which differ in
	// which attributes they emit on the root span.
	OperationType string
	FinishReason  string
	Usage         TelemetryUsage
	ModelProvider string
	ModelID       string
	// Text is the full generated text. Integrations should check
	// Settings.RecordOutputs before recording this value.
	Text string
	// Reasoning is the joined reasoning/thinking text for the final step,
	// emitted as ai.response.reasoning (output-gated) for ai.generateText/
	// ai.streamText, mirroring TS onGenerateEnd's
	// `event.finalStep.reasoning`.
	Reasoning string
	// ToolCalls made in the final step, emitted as ai.response.toolCalls
	// (output-gated) for ai.generateText/ai.streamText only.
	ToolCalls []types.ToolCall
	// Files holds any model-generated output files (e.g. images, audio).
	// Integrations should check Settings.RecordOutputs before recording file data.
	Files []types.GeneratedFileContent
	// Object holds the parsed result for ai.generateObject/ai.streamObject,
	// emitted as ai.response.object (output-gated, JSON-encoded) instead of
	// ai.response.text/reasoning/toolCalls/files, mirroring TS's
	// onObjectOperationEnd.
	Object interface{}
	// Embedding holds the embed/embedMany result: a single []float64 for
	// ai.embed, or a [][]float64 for ai.embedMany, emitted as ai.embedding /
	// ai.embeddings respectively (output-gated, JSON-encoded per value),
	// mirroring TS's onEmbedOperationEnd. Not used by other operations.
	Embedding      interface{}
	Settings       *Settings
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
	// ProviderMetadata holds the final step's provider-specific response
	// metadata, emitted as ai.response.providerMetadata when
	// OpenTelemetryOptions.ProviderMetadata is set (18651f6).
	ProviderMetadata map[string]interface{}
}

// TelemetryErrorEvent is passed to TelemetryIntegration.OnError.
type TelemetryErrorEvent struct {
	Settings *Settings
	Error    error
	// CallID, when set, lets an integration defensively close a nested span
	// it opened for this call (e.g. the "ai.evaluate.doEvaluate" span from
	// OnEvaluationModelCallStart) that would otherwise leak because the
	// corresponding *End event is never notified on an error path. Other
	// operations leave this empty and are unaffected.
	CallID string
}

// TelemetryAbortEvent is emitted when a generation is aborted by context
// cancellation or timeout.
type TelemetryAbortEvent struct {
	Settings *Settings
	CallID   string
	Reason   error
	Steps    []types.StepResult
}

// TelemetryUsage carries token counts for telemetry events.
type TelemetryUsage struct {
	InputTokens              *int64
	OutputTokens             *int64
	TotalTokens              *int64
	CacheReadInputTokens     *int64
	CacheCreationInputTokens *int64
	ReasoningTokens          *int64
	// NoCacheInputTokens tracks tokens that bypassed the cache (ai.usage.inputTokenDetails.noCacheTokens).
	NoCacheInputTokens *int64
	// OutputTextTokens tracks text-only output tokens (ai.usage.outputTokenDetails.textTokens).
	OutputTextTokens *int64
}

// ---------------------------------------------------------------------------
// TelemetryIntegration interface
// ---------------------------------------------------------------------------

// TelemetryIntegration receives lifecycle events from AI operations and
// translates them into backend-specific observability records (OTel spans,
// metrics, logs, etc.).
//
// Implementations must be safe for concurrent use — multiple goroutines may
// call any method simultaneously.
//
// The interface mirrors the TypeScript AI SDK's TelemetryIntegration type:
// all methods map 1-to-1, with Go-idiomatic naming.
type TelemetryIntegration interface {
	// OnStart is called once before the first LLM request.
	// Implementations that create a root span should embed it in the returned
	// context (e.g. via trace.ContextWithSpan) so downstream methods can
	// retrieve it with trace.SpanFromContext.
	OnStart(ctx context.Context, e TelemetryStartEvent) context.Context

	// OnStepStart is called at the beginning of each LLM step.
	// Implementations that create a per-step child span should embed it in the
	// returned context so OnStepEnd can retrieve and end it.
	OnStepStart(ctx context.Context, e TelemetryStepStartEvent) context.Context

	// OnToolExecutionStart is called just before each tool's Execute function runs.
	// TypeScript equivalent: onToolExecutionStart.
	// Return a (possibly modified) context; OTel implementations may start a
	// child span and embed it for OnToolExecutionEnd.
	OnToolExecutionStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context

	// OnToolExecutionEnd is called after each tool's Execute function returns,
	// whether the execution succeeded or failed.
	// TypeScript equivalent: onToolExecutionEnd.
	OnToolExecutionEnd(ctx context.Context, e TelemetryToolCallFinishEvent)

	// OnToolCallStart is the previous Go name for OnToolExecutionStart.
	//
	// Deprecated: use OnToolExecutionStart.
	OnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context

	// OnToolCallFinish is the previous Go name for OnToolExecutionEnd.
	//
	// Deprecated: use OnToolExecutionEnd.
	OnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent)

	// OnChunk is retained for source compatibility with older integrations.
	// New telemetry dispatchers do not emit chunk events.
	//
	// Deprecated: use OnStepEnd, OnLanguageModelCallEnd, and OnEnd.
	OnChunk(ctx context.Context, e TelemetryChunkEvent)

	// OnStepEnd is called after each LLM step completes.
	OnStepEnd(ctx context.Context, e TelemetryStepEndEvent)

	// OnFinish is called once when the AI operation completes successfully.
	// OTel implementations should end the root span here.
	//
	// Deprecated: implement OnEnd instead.
	OnFinish(ctx context.Context, e TelemetryFinishEvent)

	// OnError is called when the AI operation fails with an error.
	// OTel implementations should record the error on the span and end it.
	OnError(ctx context.Context, e TelemetryErrorEvent)

	// ExecuteTool wraps tool execution, enabling integrations to create nested
	// child spans for tool→generateText chains.
	// Implementations MUST call execute and return its result unchanged.
	// The default (NoopTelemetryIntegration) delegates directly to execute.
	ExecuteTool(
		ctx context.Context,
		toolName string,
		args map[string]interface{},
		execute func(ctx context.Context, args map[string]interface{}) (interface{}, error),
	) (interface{}, error)
}

// Telemetry is the stable name for telemetry integrations.
type Telemetry = TelemetryIntegration

type languageModelCallStartHandler interface {
	OnLanguageModelCallStart(context.Context, LanguageModelCallStartEvent) context.Context
}

type languageModelCallEndHandler interface {
	OnLanguageModelCallEnd(context.Context, LanguageModelCallEndEvent)
}

type abortHandler interface {
	OnAbort(context.Context, TelemetryAbortEvent)
}

// stepErrorHandler is implemented by integrations that close a still-open
// step (and, for GenAI, model-call/"chat") span when the provider call
// itself failed — see FireOnStepError.
type stepErrorHandler interface {
	OnStepError(context.Context, TelemetryErrorEvent)
}

type endHandler interface {
	OnEnd(context.Context, TelemetryFinishEvent)
}

type embedStartHandler interface {
	OnEmbedStart(context.Context, EmbeddingModelCallStartEvent)
}

type embedFinishHandler interface {
	OnEmbedFinish(context.Context, EmbeddingModelCallEndEvent)
}

type embedEndHandler interface {
	OnEmbedEnd(context.Context, EmbeddingModelCallEndEvent)
}

type rerankStartHandler interface {
	OnRerankStart(context.Context, RerankingModelCallStartEvent)
}

type rerankFinishHandler interface {
	OnRerankFinish(context.Context, RerankingModelCallEndEvent)
}

type rerankEndHandler interface {
	OnRerankEnd(context.Context, RerankingModelCallEndEvent)
}

// ---------------------------------------------------------------------------
// NoopTelemetryIntegration
// ---------------------------------------------------------------------------

// NoopTelemetryIntegration implements TelemetryIntegration with all no-ops.
// It is used as the default when no integration has been registered, and by
// callers that want to temporarily suppress telemetry.
type NoopTelemetryIntegration struct{}

func (NoopTelemetryIntegration) OnStart(ctx context.Context, _ TelemetryStartEvent) context.Context {
	return ctx
}
func (NoopTelemetryIntegration) OnStepStart(ctx context.Context, _ TelemetryStepStartEvent) context.Context {
	return ctx
}
func (NoopTelemetryIntegration) OnToolExecutionStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	return ctx
}
func (NoopTelemetryIntegration) OnToolExecutionEnd(_ context.Context, _ TelemetryToolCallFinishEvent) {
}
func (NoopTelemetryIntegration) OnToolCallStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	return ctx
}
func (NoopTelemetryIntegration) OnToolCallFinish(_ context.Context, _ TelemetryToolCallFinishEvent) {
}
func (NoopTelemetryIntegration) OnChunk(_ context.Context, _ TelemetryChunkEvent)     {}
func (NoopTelemetryIntegration) OnStepEnd(_ context.Context, _ TelemetryStepEndEvent) {}
func (NoopTelemetryIntegration) OnFinish(_ context.Context, _ TelemetryFinishEvent)   {}
func (NoopTelemetryIntegration) OnError(_ context.Context, _ TelemetryErrorEvent)     {}
func (NoopTelemetryIntegration) OnAbort(_ context.Context, _ TelemetryAbortEvent)     {}
func (NoopTelemetryIntegration) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}

// ---------------------------------------------------------------------------
// OTelTelemetryIntegration
// ---------------------------------------------------------------------------

// LegacyOpenTelemetry translates TelemetryIntegration events into the SDK's
// original "ai.*"-shaped OpenTelemetry spans, matching TS's
// `otel/src/legacy-open-telemetry.ts` LegacyOpenTelemetry class. Register it
// (or construct one with NewLegacyOpenTelemetry to configure a tracer or
// EnrichSpan) to enable OTel tracing:
//
//	telemetry.RegisterTelemetryIntegration(telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{}))
//
// The zero value is valid and uses the global OTel tracer provider, matching
// TS's `new LegacyOpenTelemetry()` with no options.
type LegacyOpenTelemetry struct {
	tracer     trace.Tracer
	enrichSpan EnrichSpanFunc
}

// LegacyOpenTelemetryOptions configures a LegacyOpenTelemetry integration.
type LegacyOpenTelemetryOptions struct {
	// Tracer is the OTel tracer to use. Defaults to the global tracer
	// provider's "ai-sdk" tracer when nil.
	Tracer trace.Tracer
	// EnrichSpan adds custom attributes to spans as they are created.
	// SDK-managed attributes win on key collisions. A per-call
	// Settings.EnrichSpan, when set, overrides this constructor-level value.
	EnrichSpan EnrichSpanFunc
}

// NewLegacyOpenTelemetry creates a LegacyOpenTelemetry integration
// configured with a tracer and/or EnrichSpan function, matching TS's
// `new LegacyOpenTelemetry({tracer, enrichSpan})` (9b47dea).
func NewLegacyOpenTelemetry(opts LegacyOpenTelemetryOptions) LegacyOpenTelemetry {
	return LegacyOpenTelemetry{tracer: opts.Tracer, enrichSpan: opts.EnrichSpan}
}

// tracerFor resolves the tracer this integration should use: a no-op tracer
// when telemetry is disabled, the constructor-configured tracer when set,
// else the shared GetTracer fallback (global "ai-sdk" tracer).
func (i LegacyOpenTelemetry) tracerFor(settings *Settings) trace.Tracer {
	if !Enabled(settings) {
		return noop.NewTracerProvider().Tracer(TracerName)
	}
	if i.tracer != nil {
		return i.tracer
	}
	return GetTracer(settings)
}

// OTelTelemetryIntegration is a deprecated alias for LegacyOpenTelemetry.
//
// Deprecated: use LegacyOpenTelemetry / NewLegacyOpenTelemetry.
type OTelTelemetryIntegration = LegacyOpenTelemetry

type otelSpanEntry struct {
	span trace.Span
	// toolDefs is used only by the GenAI OpenTelemetry integration's
	// languageModel span entries: the declared tool definitions captured at
	// OnLanguageModelCallStart, merged with provider-executed tool calls
	// observed at OnLanguageModelCallEnd (5ad6abf).
	toolDefs []map[string]interface{}
}

var otelModelCallSpans sync.Map

func otelSpanKey(kind, callID string) string {
	return kind + ":" + callID
}

func modelCallID(parts ...string) string {
	for _, part := range parts {
		if part != "" {
			return part
		}
	}
	return ""
}

// finiteFloat64Attr returns an attribute.KeyValue for a float64 value, or
// false when the value is NaN or ±Inf. OTLP does not support non-finite
// floats; mirrors TS sanitizeAttributeValue (otel/src/sanitize-attribute-value.ts).
func finiteFloat64Attr(key string, v float64) (attribute.KeyValue, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return attribute.KeyValue{}, false
	}
	return attribute.Float64(key, v), true
}

// finiteFloat64Slice returns values unchanged, or false if any element is
// NaN/±Inf (TS drops the whole array in that case; see sanitizeAttributeValue).
func finiteFloat64Slice(values []float64) ([]float64, bool) {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false
		}
	}
	return values, true
}

// setFiniteFloat64 appends a Float64 attribute.KeyValue to attrs, skipping
// non-finite values (NaN/±Inf), which are otherwise silently dropped or
// rejected by OTLP exporters. Common source: tokens-per-second metrics
// computed with a zero-duration denominator.
func setFiniteFloat64(attrs []attribute.KeyValue, key string, v float64) []attribute.KeyValue {
	if kv, ok := finiteFloat64Attr(key, v); ok {
		return append(attrs, kv)
	}
	return attrs
}

// customSpanAttributes builds attributes from an EnrichSpan function. A
// per-call Settings.EnrichSpan takes precedence over the integration's
// constructor-level EnrichSpan (ctorEnrich), matching TS's per-call/
// constructor EnrichSpan precedence (c025d60).
func customSpanAttributes(ctx context.Context, ctorEnrich EnrichSpanFunc, settings *Settings, opts EnrichSpanOptions) []attribute.KeyValue {
	enrich := ctorEnrich
	if settings != nil && settings.EnrichSpan != nil {
		enrich = settings.EnrichSpan
	}
	if enrich == nil {
		return nil
	}
	defer func() {
		_ = recover()
	}()
	attrs := enrich(ctx, opts)
	if len(attrs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for key, value := range attrs {
		switch v := value.(type) {
		case string:
			out = append(out, attribute.String(key, v))
		case bool:
			out = append(out, attribute.Bool(key, v))
		case int:
			out = append(out, attribute.Int(key, v))
		case int64:
			out = append(out, attribute.Int64(key, v))
		case float64:
			if kv, ok := finiteFloat64Attr(key, v); ok {
				out = append(out, kv)
			}
		case []string:
			out = append(out, attribute.StringSlice(key, v))
		case []bool:
			out = append(out, attribute.BoolSlice(key, v))
		case []int:
			out = append(out, attribute.IntSlice(key, v))
		case []int64:
			out = append(out, attribute.Int64Slice(key, v))
		case []float64:
			if finite, ok := finiteFloat64Slice(v); ok {
				out = append(out, attribute.Float64Slice(key, finite))
			}
		default:
			if b, err := json.Marshal(v); err == nil {
				out = append(out, attribute.String(key, string(b)))
			}
		}
	}
	return out
}

// settingsFunctionID returns settings.FunctionID, or "" when settings is nil.
func settingsFunctionID(settings *Settings) string {
	if settings == nil {
		return ""
	}
	return settings.FunctionID
}

// legacyOperationNameAttrs mirrors TS's assembleOperationName
// (otel/src/assemble-operation-name.ts): operation.name is "<operationID>
// <functionID>" when functionID is set, else just operationID; resource.name
// and ai.telemetry.functionId are only set when functionID is non-empty (TS:
// `telemetry?.functionId` is undefined otherwise, and selectAttributes drops
// undefined values). The real OTel span name is never suffixed with
// functionID — TS always names spans after the bare operation id
// (`this.tracer.startSpan(event.operationId, ...)`); functionID surfaces
// only through these attributes.
func legacyOperationNameAttrs(operationID, functionID string) []attribute.KeyValue {
	opName := operationID
	if functionID != "" {
		opName += " " + functionID
	}
	attrs := []attribute.KeyValue{
		attribute.String("operation.name", opName),
		attribute.String("ai.operationId", operationID),
	}
	if functionID != "" {
		attrs = append(attrs,
			attribute.String("resource.name", functionID),
			attribute.String("ai.telemetry.functionId", functionID),
		)
	}
	return attrs
}

// legacySettings holds the call-level generation settings TS folds into a
// plain "settings" object before getBaseTelemetryAttributes
// (legacy-open-telemetry.ts onGenerateStart/onObjectOperationStart/
// onEmbedOperationStart/onRerankOperationStart/onEvaluateOperationStart).
// Every operation sets MaxRetries; only generateText/streamText set
// StopSequences.
type legacySettings struct {
	MaxOutputTokens  *int
	Temperature      *float64
	TopP             *float64
	TopK             *int
	PresencePenalty  *float64
	FrequencyPenalty *float64
	StopSequences    []string
	Seed             *int
	MaxRetries       *int
}

// legacySettingsAttrs builds "ai.settings.<key>" attributes, skipping unset
// (nil) fields — mirroring selectAttributes() dropping settings-object
// entries whose value is undefined.
func legacySettingsAttrs(s legacySettings) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if s.MaxOutputTokens != nil {
		attrs = append(attrs, attribute.Int("ai.settings.maxOutputTokens", *s.MaxOutputTokens))
	}
	if s.Temperature != nil {
		if kv, ok := finiteFloat64Attr("ai.settings.temperature", *s.Temperature); ok {
			attrs = append(attrs, kv)
		}
	}
	if s.TopP != nil {
		if kv, ok := finiteFloat64Attr("ai.settings.topP", *s.TopP); ok {
			attrs = append(attrs, kv)
		}
	}
	if s.TopK != nil {
		attrs = append(attrs, attribute.Int("ai.settings.topK", *s.TopK))
	}
	if s.PresencePenalty != nil {
		if kv, ok := finiteFloat64Attr("ai.settings.presencePenalty", *s.PresencePenalty); ok {
			attrs = append(attrs, kv)
		}
	}
	if s.FrequencyPenalty != nil {
		if kv, ok := finiteFloat64Attr("ai.settings.frequencyPenalty", *s.FrequencyPenalty); ok {
			attrs = append(attrs, kv)
		}
	}
	if s.StopSequences != nil {
		attrs = append(attrs, attribute.StringSlice("ai.settings.stopSequences", s.StopSequences))
	}
	if s.Seed != nil {
		attrs = append(attrs, attribute.Int("ai.settings.seed", *s.Seed))
	}
	if s.MaxRetries != nil {
		attrs = append(attrs, attribute.Int("ai.settings.maxRetries", *s.MaxRetries))
	}
	return attrs
}

// legacyBaseAttrs builds ai.model.provider, ai.model.id, ai.settings.<key>,
// and ai.request.headers.<name>, mirroring TS's getBaseTelemetryAttributes
// (otel/src/get-base-telemetry-attributes.ts). Runtime context attrs
// (ai.settings.context.*) are handled separately by runtimeContextAttributes
// since callers need that slice standalone to stash for descendant spans.
func legacyBaseAttrs(modelProvider, modelID string, settings legacySettings, headers map[string]string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("ai.model.provider", modelProvider),
		attribute.String("ai.model.id", modelID),
	}
	attrs = append(attrs, legacySettingsAttrs(settings)...)
	for k, v := range headers {
		attrs = append(attrs, attribute.String("ai.request.headers."+k, v))
	}
	return attrs
}

// legacyJSONEachElement JSON-encodes each element of a slice-typed value
// individually, mirroring TS's `values.map(v => JSON.stringify(v))` used for
// ai.values (embedMany) and ai.documents (rerank, whose documents may be
// []string or []map[string]interface{}). Returns nil for unsupported or nil
// input.
func legacyJSONEachElement(v interface{}) []string {
	switch docs := v.(type) {
	case []string:
		out := make([]string, len(docs))
		for i, d := range docs {
			out[i] = legacyJSONOrEmpty(d)
		}
		return out
	case []map[string]interface{}:
		out := make([]string, len(docs))
		for i, d := range docs {
			out[i] = legacyJSONOrEmpty(d)
		}
		return out
	case []interface{}:
		out := make([]string, len(docs))
		for i, d := range docs {
			out[i] = legacyJSONOrEmpty(d)
		}
		return out
	default:
		return nil
	}
}

// legacyJSONOrEmpty JSON-encodes v, returning "" if it cannot be marshaled.
func legacyJSONOrEmpty(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// legacyPromptJSON builds the "ai.prompt" JSON attribute value for
// generateText/streamText/generateObject/streamObject root spans, mirroring
// TS's onGenerateStart (`JSON.stringify({system, messages})`) and
// onObjectOperationStart (`JSON.stringify({system, prompt, messages})`) in
// legacy-open-telemetry.ts. Field order matches the TS object-literal
// insertion order (Go struct fields marshal in declared order, unlike map
// keys which json.Marshal sorts alphabetically); `omitempty` mirrors
// JSON.stringify dropping undefined fields. Unlike TS — whose ModelMessage
// content can be a bare string for simple text — Go's types.Message.Content
// is always a slice of parts, so a simple single-text message serializes as
// an array-of-parts object rather than a bare string; this is an accepted
// Go-runtime type-system divergence (same one already present in
// ai.prompt.messages / ai.prompt.tools elsewhere).
func legacyPromptJSON(isObjectOp bool, system, prompt string, messages []types.Message) string {
	if isObjectOp {
		v := struct {
			System   string          `json:"system,omitempty"`
			Prompt   string          `json:"prompt,omitempty"`
			Messages []types.Message `json:"messages,omitempty"`
		}{System: system, Prompt: prompt, Messages: messages}
		return legacyJSONOrEmpty(v)
	}
	v := struct {
		System   string          `json:"system,omitempty"`
		Messages []types.Message `json:"messages,omitempty"`
	}{System: system, Messages: messages}
	return legacyJSONOrEmpty(v)
}

// legacyBaseAttrsKey is a private context key carrying the root span's
// ai.model.provider/id + ai.settings.* + ai.request.headers.* attributes
// down to the nested doGenerate/doStream step span, mirroring TS's reuse of
// `state.baseTelemetryAttributes` in onStepStart.
type legacyBaseAttrsKey struct{}

// OnStart starts the root OTel span and embeds it in the returned context.
// Returns ctx unchanged when settings explicitly disables telemetry.
// Attribute shape mirrors TS's onGenerateStart / onObjectOperationStart /
// onEmbedOperationStart / onRerankOperationStart (legacy-open-telemetry.ts):
// ai.model.provider/id, ai.settings.<key>, ai.request.headers.<name>, and
// operation.name/resource.name/ai.telemetry.functionId (assembleOperationName)
// are emitted for every operation; gen_ai.system/gen_ai.request.model are
// NOT part of the TS root span (they only appear on the nested
// doGenerate/doStream step span) and are no longer emitted here.
func (i LegacyOpenTelemetry) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := i.tracerFor(e.Settings)
	ctx, span := tracer.Start(ctx, e.OperationType)
	if attrs := customSpanAttributes(ctx, i.enrichSpan, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationType,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	functionID := ""
	if e.Settings != nil {
		functionID = e.Settings.FunctionID
	}
	span.SetAttributes(legacyOperationNameAttrs(e.OperationType, functionID)...)

	settings := legacySettings{
		MaxOutputTokens:  e.MaxOutputTokens,
		Temperature:      e.Temperature,
		TopP:             e.TopP,
		TopK:             e.TopK,
		PresencePenalty:  e.PresencePenalty,
		FrequencyPenalty: e.FrequencyPenalty,
		Seed:             e.Seed,
		MaxRetries:       e.MaxRetries,
	}
	if e.OperationType == "ai.generateText" || e.OperationType == "ai.streamText" {
		settings.StopSequences = e.StopSequences
	}
	baseAttrs := legacyBaseAttrs(e.ModelProvider, e.ModelID, settings, e.Headers)
	span.SetAttributes(baseAttrs...)
	// Stashed so the nested doGenerate/doStream step span can reuse the same
	// base attributes, matching TS's state.baseTelemetryAttributes reuse.
	ctx = context.WithValue(ctx, legacyBaseAttrsKey{}, baseAttrs)

	recordInputs := e.Settings == nil || e.Settings.RecordInputs

	switch e.OperationType {
	case "ai.embed":
		// TS onEmbedOperationStart sets ai.value (not ai.prompt) for a
		// single embed call, input-gated and JSON-encoded.
		if recordInputs && e.Prompt != "" {
			span.SetAttributes(attribute.String("ai.value", legacyJSONOrEmpty(e.Prompt)))
		}
	case "ai.embedMany":
		// TS sets ai.values (array of individually JSON-encoded strings),
		// input-gated — not ai.prompt and not a bare count.
		if recordInputs {
			if values := legacyJSONEachElement(e.Values); len(values) > 0 {
				span.SetAttributes(attribute.StringSlice("ai.values", values))
			}
		}
	case "ai.rerank":
		// TS sets ai.documents (array of individually JSON-encoded
		// strings), input-gated — no ai.prompt/query attribute at all.
		if recordInputs {
			if docs := legacyJSONEachElement(e.Documents); len(docs) > 0 {
				span.SetAttributes(attribute.StringSlice("ai.documents", docs))
			}
		}
	default:
		// generateText/streamText/generateObject/streamObject: ai.prompt is
		// a JSON object (see legacyPromptJSON), not the bare prompt string.
		// TS always calls JSON.stringify(...) here when recordInputs is
		// true (selectAttributes only checks `resolved != null`, and the
		// object literal is never nullish), so this is unconditional on
		// content — matching that rather than gating on any field being
		// non-empty.
		isObjectOp := e.OperationType == "ai.generateObject" || e.OperationType == "ai.streamObject"
		if recordInputs {
			if promptJSON := legacyPromptJSON(isObjectOp, e.System, e.Prompt, e.Messages); promptJSON != "" {
				span.SetAttributes(attribute.String("ai.prompt", promptJSON))
			}
		}
		if isObjectOp {
			// TS: 'ai.schema' is input-gated; schema.name/description and
			// settings.output are plain (unconditional) values.
			if recordInputs && len(e.Schema) > 0 {
				if b, err := json.Marshal(e.Schema); err == nil {
					span.SetAttributes(attribute.String("ai.schema", string(b)))
				}
			}
			if e.SchemaName != "" {
				span.SetAttributes(attribute.String("ai.schema.name", e.SchemaName))
			}
			if e.SchemaDescription != "" {
				span.SetAttributes(attribute.String("ai.schema.description", e.SchemaDescription))
			}
			if e.SettingsOutput != "" {
				span.SetAttributes(attribute.String("ai.settings.output", e.SettingsOutput))
			}
		}
	}

	if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
		span.SetAttributes(attrs...)
		// Stashed so descendant spans (e.g. tool call spans) can also carry
		// the flattened ai.settings.context.* attrs, matching TS's
		// baseTelemetryAttributes reuse (0651c5f). RuntimeContext here is
		// already filtered by Settings.IncludeRuntimeContext upstream.
		ctx = context.WithValue(ctx, runtimeContextAttrsKey{}, attrs)
	}
	return ctx // span is embedded via OTel context propagation
}

// runtimeContextAttrsKey is a private context key carrying the root span's
// flattened ai.settings.context.* attributes down to descendant spans.
type runtimeContextAttrsKey struct{}

// runtimeContextAttributes flattens a runtime context map into
// "ai.settings.context.<path>" attributes, mirroring TS's
// getRuntimeContextAttributes (otel/src/supplemental-attributes.ts): nested
// objects recurse, arrays and other primitives are kept as-is, and nil
// values are skipped.
func runtimeContextAttributes(runtimeContext map[string]interface{}) []attribute.KeyValue {
	if len(runtimeContext) == 0 {
		return nil
	}
	var attrs []attribute.KeyValue
	for key, value := range runtimeContext {
		attrs = appendRuntimeContextAttribute(attrs, "ai.settings.context."+key, value)
	}
	return attrs
}

func appendRuntimeContextAttribute(attrs []attribute.KeyValue, key string, value interface{}) []attribute.KeyValue {
	if value == nil {
		return attrs
	}
	if nested, ok := value.(map[string]interface{}); ok {
		for nestedKey, nestedValue := range nested {
			attrs = appendRuntimeContextAttribute(attrs, key+"."+nestedKey, nestedValue)
		}
		return attrs
	}
	switch v := value.(type) {
	case string:
		return append(attrs, attribute.String(key, v))
	case bool:
		return append(attrs, attribute.Bool(key, v))
	case int:
		return append(attrs, attribute.Int(key, v))
	case int64:
		return append(attrs, attribute.Int64(key, v))
	case float64:
		if kv, ok := finiteFloat64Attr(key, v); ok {
			return append(attrs, kv)
		}
		return attrs
	case []string:
		return append(attrs, attribute.StringSlice(key, v))
	case []bool:
		return append(attrs, attribute.BoolSlice(key, v))
	case []int:
		return append(attrs, attribute.IntSlice(key, v))
	case []int64:
		return append(attrs, attribute.Int64Slice(key, v))
	case []float64:
		if finite, ok := finiteFloat64Slice(v); ok {
			return append(attrs, attribute.Float64Slice(key, finite))
		}
		return attrs
	default:
		if b, err := json.Marshal(v); err == nil {
			return append(attrs, attribute.String(key, string(b)))
		}
		return attrs
	}
}

// stepSpanKey is a private context key used to pass the OTel step span from
// OnStepStart to OnStepFinish without relying on trace.SpanFromContext (which
// would return the innermost span, potentially set by provider-level tracing).
type stepSpanKey struct{}

// legacyStepOperationID returns the TS-equivalent nested step operation id
// ("ai.generateText.doGenerate" / "ai.streamText.doStream" /
// "ai.generateObject.doGenerate" / "ai.streamObject.doStream"), mirroring
// TS's stepOperationId ternary in onStepStart/onObjectStepStart
// (legacy-open-telemetry.ts). Used only for the ai.operationId/
// operation.name attributes — Go keeps its own descriptive step span name
// (see OnStepStart) rather than reusing this as the literal span name.
func legacyStepOperationID(operationType string) string {
	switch operationType {
	case "ai.streamText":
		return "ai.streamText.doStream"
	case "ai.streamObject":
		return "ai.streamObject.doStream"
	case "ai.generateObject":
		return "ai.generateObject.doGenerate"
	default:
		return "ai.generateText.doGenerate"
	}
}

// OnStepStart creates a child OTel span for the step and embeds it in the
// returned context via stepSpanKey, mirroring the TS SDK's onStepStart span.
// In addition to the pre-existing gen_ai.request.model/gen_ai.system
// attributes (dual-emitted alongside ai.* in TS), it now reuses the root
// span's ai.model.provider/id + ai.settings.* + ai.request.headers.*
// (stashed via legacyBaseAttrsKey by OnStart, mirroring TS's
// state.baseTelemetryAttributes reuse) and adds
// operation.name/resource.name/ai.telemetry.functionId/ai.operationId for
// the nested doGenerate/doStream operation (assembleOperationName).
func (i LegacyOpenTelemetry) OnStepStart(ctx context.Context, e TelemetryStepStartEvent) context.Context {
	rootSpan := trace.SpanFromContext(ctx)
	if !rootSpan.IsRecording() {
		return ctx
	}
	tracer := rootSpan.TracerProvider().Tracer("go-ai")
	opType := e.OperationType
	if opType == "" {
		opType = "ai.step"
	}
	// The span's real Name is the bare step operation id (e.g.
	// "ai.generateText.doGenerate", "ai.streamText.doStream"), matching TS's
	// `this.tracer.startSpan(stepOperationId, ...)` (legacy-open-telemetry.ts
	// onStepStart/onObjectStepStart) and the snapshot span names (e.g.
	// "ai.streamText.doStream"). This mirrors the root span convention (bare
	// operationId as Name, functionID-suffixed only in the operation.name
	// attribute below).
	spanName := legacyStepOperationID(opType)
	ctx, stepSpan := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, i.enrichSpan, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeStep,
		OperationType:  opType,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		stepSpan.SetAttributes(attrs...)
	}
	stepSpan.SetAttributes(
		attribute.String("gen_ai.request.model", e.ModelID),
		attribute.String("gen_ai.system", e.ModelProvider),
	)
	functionID := ""
	if e.Settings != nil {
		functionID = e.Settings.FunctionID
	}
	stepSpan.SetAttributes(legacyOperationNameAttrs(legacyStepOperationID(opType), functionID)...)
	if baseAttrs, ok := ctx.Value(legacyBaseAttrsKey{}).([]attribute.KeyValue); ok {
		stepSpan.SetAttributes(baseAttrs...)
	}
	// TS's onStepStart sets 'ai.model.provider'/'ai.model.id' from the
	// step's own event.provider/event.modelId AFTER spreading
	// state.baseTelemetryAttributes, so a step whose model differs from the
	// initial call (e.g. via a per-step model override) reports its own
	// model rather than the root's. Set these after the baseAttrs reuse
	// above so they take precedence (OTel SetAttributes keeps the latest
	// value per key).
	stepSpan.SetAttributes(
		attribute.String("ai.model.provider", e.ModelProvider),
		attribute.String("ai.model.id", e.ModelID),
	)

	// ai.prompt.messages / ai.prompt.tools / ai.prompt.toolChoice, input-gated
	// (H3 follow-up 4): mirrors TS's onStepStart (generateText/streamText) and
	// onObjectStepStart (generateObject/streamObject, ai.prompt.messages
	// only — object calls never populate StepTools/ToolChoice, so those two
	// attributes are naturally absent there).
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if recordInputs {
		if len(e.PromptMessages) > 0 {
			if b, err := json.Marshal(e.PromptMessages); err == nil {
				stepSpan.SetAttributes(attribute.String("ai.prompt.messages", string(b)))
			}
		}
		if len(e.StepTools) > 0 {
			tools := make([]string, 0, len(e.StepTools))
			for _, t := range e.StepTools {
				if b, err := json.Marshal(t); err == nil {
					tools = append(tools, string(b))
				}
			}
			if len(tools) > 0 {
				stepSpan.SetAttributes(attribute.StringSlice("ai.prompt.tools", tools))
			}
		}
		if e.ToolChoice.Type != "" {
			if b, err := json.Marshal(e.ToolChoice); err == nil {
				stepSpan.SetAttributes(attribute.String("ai.prompt.toolChoice", string(b)))
			}
		}
	}
	return context.WithValue(ctx, stepSpanKey{}, stepSpan)
}

// LegacyOpenTelemetry intentionally has no OnLanguageModelCallStart/
// OnLanguageModelCallEnd methods (H4 item 1): TS's LegacyOpenTelemetry
// (legacy-open-telemetry.ts) never implements the optional
// onLanguageModelCallStart/onLanguageModelCallEnd Telemetry hooks at all —
// only the GenAI OpenTelemetry class (open-telemetry.ts) does, where the
// nested "chat <model>" span IS the doGenerate/doStream inference span. In
// TS's Legacy integration, the step span created by onStepStart/
// onObjectStepStart (this file's OnStepStart, above) already *is* the
// doGenerate/doStream span — there is no separate nested model-call span.
// Go previously added its own LegacyOpenTelemetry.OnLanguageModelCallStart/
// End that created an extra "chat <model>" span with Go-only attributes
// (ai.response.responseTimeMs, effectiveOutputTokensPerSecond, etc. — none
// of which TS's Legacy step span carries either). Removing these methods
// means LegacyOpenTelemetry no longer satisfies languageModelCallStartHandler/
// languageModelCallEndHandler, so FireOnLanguageModelCallStart/End's
// type-assertion loop (below) silently skips it — matching TS exactly, and
// ensuring that with both LegacyOpenTelemetry and OpenTelemetry registered,
// only OpenTelemetry (GenAI) produces a "chat <model>" span. Every attribute
// TS's onStepStart/onStepEnd put on the step span from model-call data
// (gen_ai.system, gen_ai.request.model, gen_ai.response.finish_reasons,
// gen_ai.response.id, gen_ai.usage.input_tokens/output_tokens, etc.) was
// already being dual-set on the step span by OnStepStart (above) and
// OnStepEnd (below) — nothing needed to move.

// OnEmbedStart creates a child span for the nested doEmbed model call.
// Mirrors TS's onEmbedStart (legacy-open-telemetry.ts): the span is named
// after event.operationId ("ai.embed.doEmbed" / "ai.embedMany.doEmbed", set
// by pkg/ai), reuses the root span's base attributes (ai.model.provider/id +
// ai.settings.* + ai.request.headers.*, stashed by OnStart), and carries no
// gen_ai.* attributes at all — those are a Go-only addition that doesn't
// exist on this span in TS (H3 follow-up 2).
func (i LegacyOpenTelemetry) OnEmbedStart(ctx context.Context, e EmbeddingModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := e.OperationID
	if spanName == "" {
		spanName = "ai.embed.doEmbed"
	}
	_, span := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, i.enrichSpan, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeEmbedding,
		OperationType: e.OperationID,
		CallID:        callID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	functionID := settingsFunctionID(e.Settings)
	span.SetAttributes(legacyOperationNameAttrs(spanName, functionID)...)
	if baseAttrs, ok := ctx.Value(legacyBaseAttrsKey{}).([]attribute.KeyValue); ok {
		span.SetAttributes(baseAttrs...)
	}
	if (e.Settings == nil || e.Settings.RecordInputs) && len(e.Values) > 0 {
		span.SetAttributes(attribute.StringSlice("ai.values", legacyJSONEachElement(e.Values)))
	}
	if callID != "" {
		otelModelCallSpans.Store(otelSpanKey("embedding", callID), otelSpanEntry{span: span})
	}
}

// OnEmbedEnd records embedding attributes and ends the doEmbed span.
// Mirrors TS's onEmbedEnd: only ai.embeddings (output-gated) and
// ai.usage.tokens — no gen_ai.* here either.
func (i LegacyOpenTelemetry) OnEmbedEnd(_ context.Context, e EmbeddingModelCallEndEvent) {
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("embedding", callID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if (e.Settings == nil || e.Settings.RecordOutputs) && len(e.Embeddings) > 0 {
		entry.span.SetAttributes(attribute.StringSlice("ai.embeddings", jsonStringifyEach(e.Embeddings)))
	}
	// ai.usage.tokens is a plain value in TS (not {output: ...}), so it is
	// never gated by recordOutputs; sanitizeAttributeValue drops it when
	// usage.tokens is NaN (provider returned no usage), which
	// setFiniteFloat64 mirrors.
	entry.span.SetAttributes(setFiniteFloat64(nil, "ai.usage.tokens", e.Usage.Tokens)...)
	entry.span.End()
}

// OnRerankStart creates a child span for the nested doRerank model call.
// Mirrors TS's onRerankStart: span named after event.operationId
// ("ai.rerank.doRerank"), reuses the root span's base attributes, and
// carries no gen_ai.* (H3 follow-up 2).
func (i LegacyOpenTelemetry) OnRerankStart(ctx context.Context, e RerankingModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.CallID, e.OperationID)
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := e.OperationID
	if spanName == "" {
		spanName = "ai.rerank.doRerank"
	}
	_, span := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, i.enrichSpan, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeReranking,
		OperationType: e.OperationID,
		CallID:        callID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	functionID := settingsFunctionID(e.Settings)
	span.SetAttributes(legacyOperationNameAttrs(spanName, functionID)...)
	if baseAttrs, ok := ctx.Value(legacyBaseAttrsKey{}).([]attribute.KeyValue); ok {
		span.SetAttributes(baseAttrs...)
	}
	if (e.Settings == nil || e.Settings.RecordInputs) && e.Documents != nil {
		if docs := legacyJSONEachElement(e.Documents); len(docs) > 0 {
			span.SetAttributes(attribute.StringSlice("ai.documents", docs))
		}
	}
	if callID != "" {
		otelModelCallSpans.Store(otelSpanKey("reranking", callID), otelSpanEntry{span: span})
	}
}

// OnRerankEnd records reranking attributes and ends the doRerank span.
// Mirrors TS's onRerankEnd: ai.ranking.type (plain) and ai.ranking
// (output-gated) — no result count, no gen_ai.*.
func (i LegacyOpenTelemetry) OnRerankEnd(_ context.Context, e RerankingModelCallEndEvent) {
	callID := modelCallID(e.CallID, e.OperationID)
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("reranking", callID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	entry.span.SetAttributes(attribute.String("ai.ranking.type", e.DocumentsType))
	if e.Settings == nil || e.Settings.RecordOutputs {
		if len(e.Ranking) > 0 {
			entry.span.SetAttributes(attribute.StringSlice("ai.ranking", jsonStringifyEach(e.Ranking)))
		}
	}
	entry.span.End()
}

// OnToolExecutionStart starts a child span for tool execution and embeds it.
// Mirrors TS's onToolExecutionStart (legacy-open-telemetry.ts): the real
// span name is the bare "ai.toolCall" (never suffixed with the tool name —
// same span-naming rule as legacyOperationNameAttrs elsewhere), with the
// tool name carried only by the ai.toolCall.name attribute and by
// operation.name/resource.name/ai.telemetry.functionId
// (assembleOperationName({operationId: 'ai.toolCall', telemetry})).
func (i LegacyOpenTelemetry) OnToolExecutionStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx
	}
	tracer := span.TracerProvider().Tracer("go-ai")
	ctx, child := tracer.Start(ctx, "ai.toolCall")
	if attrs := customSpanAttributes(ctx, i.enrichSpan, e.Settings, EnrichSpanOptions{
		SpanType: SpanTypeTool,
		CallID:   e.ToolCallID,
	}); len(attrs) > 0 {
		child.SetAttributes(attrs...)
	}
	functionID := ""
	if e.Settings != nil {
		functionID = e.Settings.FunctionID
	}
	child.SetAttributes(legacyOperationNameAttrs("ai.toolCall", functionID)...)
	child.SetAttributes(
		attribute.String("ai.toolCall.name", e.ToolName),
		attribute.String("ai.toolCall.id", e.ToolCallID),
	)
	// ai.toolCall.args is output-gated in TS (`output: () => ...`), not
	// input-gated — the model's tool-call arguments are treated as part of
	// the generation's output.
	if (e.Settings == nil || e.Settings.RecordOutputs) && e.Args != nil {
		if b, err := json.Marshal(e.Args); err == nil {
			child.SetAttributes(attribute.String("ai.toolCall.args", string(b)))
		}
	}
	// Runtime context attrs on tool call spans (0651c5f): reuse the root
	// span's already-flattened ai.settings.context.* attrs, since
	// TelemetryToolCallStartEvent carries no RuntimeContext of its own.
	if attrs, ok := ctx.Value(runtimeContextAttrsKey{}).([]attribute.KeyValue); ok {
		child.SetAttributes(attrs...)
	}
	return ctx
}

// OnToolExecutionEnd ends the tool execution child span, mirroring TS's
// onToolExecutionEnd: a successful call records ai.toolCall.result
// (output-gated, JSON-encoded), a failed one records the error on the span
// instead. ai.toolCall.durationMs is a Go-only addition (TS carries no
// duration on this span) kept as an additive, non-conflicting attribute.
func (i LegacyOpenTelemetry) OnToolExecutionEnd(ctx context.Context, e TelemetryToolCallFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.Int64("ai.toolCall.durationMs", e.DurationMs))
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	} else if (e.Settings == nil || e.Settings.RecordOutputs) && e.Result != nil {
		if b, err := json.Marshal(e.Result); err == nil {
			span.SetAttributes(attribute.String("ai.toolCall.result", string(b)))
		}
	}
	span.End()
}

// OnToolCallStart is the previous Go name for OnToolExecutionStart.
//
// Deprecated: use OnToolExecutionStart.
func (i LegacyOpenTelemetry) OnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	return i.OnToolExecutionStart(ctx, e)
}

// OnToolCallFinish is the previous Go name for OnToolExecutionEnd.
//
// Deprecated: use OnToolExecutionEnd.
func (i LegacyOpenTelemetry) OnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent) {
	i.OnToolExecutionEnd(ctx, e)
}

func (i LegacyOpenTelemetry) OnChunk(_ context.Context, _ TelemetryChunkEvent) {}

// legacyToolCallsJSON JSON-encodes tool calls as TS's onStepEnd/onGenerateEnd
// `event.toolCalls.map(tc => ({toolCallId, toolName, input}))`.
func legacyToolCallsJSON(toolCalls []types.ToolCall) (string, bool) {
	if len(toolCalls) == 0 {
		return "", false
	}
	type toolCallEntry struct {
		ToolCallID string      `json:"toolCallId"`
		ToolName   string      `json:"toolName"`
		Input      interface{} `json:"input"`
	}
	entries := make([]toolCallEntry, len(toolCalls))
	for i, tc := range toolCalls {
		entries[i] = toolCallEntry{ToolCallID: tc.ID, ToolName: tc.ToolName, Input: tc.Arguments}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// legacyFilesJSON JSON-encodes generated files as TS's
// `event.files.map(f => ({type: 'file', mediaType, data: f.base64}))`.
func legacyFilesJSON(files []types.GeneratedFileContent) (string, bool) {
	if len(files) == 0 {
		return "", false
	}
	type fileEntry struct {
		Type      string `json:"type"`
		MediaType string `json:"mediaType"`
		Data      string `json:"data"`
	}
	entries := make([]fileEntry, len(files))
	for i, f := range files {
		entries[i] = fileEntry{Type: "file", MediaType: f.MediaType, Data: base64.StdEncoding.EncodeToString(f.Data)}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// legacyFullUsageAttrs builds the ai.usage.* attribute set TS's onStepEnd/
// onGenerateEnd (legacy-open-telemetry.ts) emit for generateText/streamText:
// inputTokens/outputTokens/totalTokens/reasoningTokens/cachedInputTokens plus
// the detailed inputTokenDetails.*/outputTokenDetails.* breakdown. No
// gen_ai.* attributes are included — callers add those separately where TS
// does (OnStepEnd only; the root OnEnd never dual-emits gen_ai.usage.*, H3
// follow-up 3).
func legacyFullUsageAttrs(u TelemetryUsage) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if u.InputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.inputTokens", *u.InputTokens))
	}
	if u.OutputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.outputTokens", *u.OutputTokens))
	}
	if u.TotalTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.totalTokens", *u.TotalTokens))
	}
	if u.ReasoningTokens != nil {
		attrs = append(attrs,
			attribute.Int64("ai.usage.reasoningTokens", *u.ReasoningTokens),
			attribute.Int64("ai.usage.outputTokenDetails.reasoningTokens", *u.ReasoningTokens),
		)
	}
	if u.CacheReadInputTokens != nil {
		attrs = append(attrs,
			attribute.Int64("ai.usage.cachedInputTokens", *u.CacheReadInputTokens),
			attribute.Int64("ai.usage.inputTokenDetails.cacheReadTokens", *u.CacheReadInputTokens),
		)
	}
	if u.CacheCreationInputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.inputTokenDetails.cacheWriteTokens", *u.CacheCreationInputTokens))
	}
	if u.NoCacheInputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.inputTokenDetails.noCacheTokens", *u.NoCacheInputTokens))
	}
	if u.OutputTextTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.outputTokenDetails.textTokens", *u.OutputTextTokens))
	}
	return attrs
}

// legacyObjectUsageAttrs builds the smaller ai.usage.* set TS's
// onObjectStepEnd/onObjectOperationEnd emit for generateObject/streamObject:
// inputTokens/outputTokens/totalTokens/reasoningTokens/cachedInputTokens
// only — no inputTokenDetails.*/outputTokenDetails.* breakdown.
func legacyObjectUsageAttrs(u TelemetryUsage) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if u.InputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.inputTokens", *u.InputTokens))
	}
	if u.OutputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.outputTokens", *u.OutputTokens))
	}
	if u.TotalTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.totalTokens", *u.TotalTokens))
	}
	if u.ReasoningTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.reasoningTokens", *u.ReasoningTokens))
	}
	if u.CacheReadInputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.cachedInputTokens", *u.CacheReadInputTokens))
	}
	return attrs
}

// isLegacyObjectOperation reports whether operationType is generateObject or
// streamObject, the operations that use the deprecated-but-still-dispatched
// onObjectStepStart/onObjectStepEnd/onObjectOperationEnd attribute shapes.
func isLegacyObjectOperation(operationType string) bool {
	return operationType == "ai.generateObject" || operationType == "ai.streamObject"
}

// OnStepEnd records step-level OTel attributes on the child step span created
// by OnStepStart and ends the span. Mirrors the TS SDK's onStepEnd
// (generateText/streamText) and, for generateObject/streamObject, the
// deprecated-but-still-dispatched onObjectStepEnd — a materially different
// shape (ai.response.object instead of text/reasoning/toolCalls/files, and
// the smaller legacyObjectUsageAttrs usage set), matching TS's dispatch on
// state.operationId (H3 follow-up 3).
func (i LegacyOpenTelemetry) OnStepEnd(ctx context.Context, e TelemetryStepEndEvent) {
	stepSpan, ok := ctx.Value(stepSpanKey{}).(trace.Span)
	if !ok || !stepSpan.IsRecording() {
		return
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs

	stepSpan.SetAttributes(attribute.String("ai.response.finishReason", e.FinishReason))

	if isLegacyObjectOperation(e.OperationType) {
		// onObjectStepEnd: ai.response.object (re-parsed/re-stringified JSON
		// text, falling back to the raw text on parse failure, mirroring TS's
		// try/catch), no reasoning/toolCalls/files.
		if recordOutputs && e.Text != "" {
			objectAttr := e.Text
			var parsed interface{}
			if json.Unmarshal([]byte(e.Text), &parsed) == nil {
				if b, err := json.Marshal(parsed); err == nil {
					objectAttr = string(b)
				}
			}
			stepSpan.SetAttributes(attribute.String("ai.response.object", objectAttr))
		}
		// onObjectStepEnd sets ai.stream.msToFirstChunk directly (bypassing
		// the recordOutputs-gated attribute set entirely, like TS) and adds
		// an "ai.stream.firstChunk" span event, only when the step actually
		// streamed a first chunk — always nil for non-streaming
		// generateObject, matching TS's `msToFirstChunk: undefined` there.
		if e.Performance.TimeToFirstOutputMs != nil {
			ms := *e.Performance.TimeToFirstOutputMs
			stepSpan.SetAttributes(attribute.Int64("ai.stream.msToFirstChunk", ms))
			stepSpan.AddEvent("ai.stream.firstChunk", trace.WithAttributes(
				attribute.Int64("ai.stream.msToFirstChunk", ms),
			))
		}
	} else {
		if recordOutputs && e.Text != "" {
			stepSpan.SetAttributes(attribute.String("ai.response.text", e.Text))
		}
		if recordOutputs && e.Reasoning != "" {
			stepSpan.SetAttributes(attribute.String("ai.response.reasoning", e.Reasoning))
		}
		if recordOutputs {
			if s, ok := legacyToolCallsJSON(e.ToolCalls); ok {
				stepSpan.SetAttributes(attribute.String("ai.response.toolCalls", s))
			}
			if s, ok := legacyFilesJSON(e.Files); ok {
				stepSpan.SetAttributes(attribute.String("ai.response.files", s))
			}
		}
		// ai.response.msToFirstChunk/msToFinish/avgOutputTokensPerSecond and
		// the matching "ai.stream.firstChunk"/"ai.stream.finish" span events
		// are ai.streamText-only (TS's onStepEnd `isStreamText` gate) and,
		// like the msTo* fields above, are plain values in TS's
		// selectAttributes call, not {output: ...} — so they're never gated
		// by recordOutputs.
		isStreamText := e.OperationType == "ai.streamText"
		if isStreamText {
			if e.Performance.TimeToFirstOutputMs != nil {
				ms := *e.Performance.TimeToFirstOutputMs
				stepSpan.SetAttributes(attribute.Int64("ai.response.msToFirstChunk", ms))
				stepSpan.AddEvent("ai.stream.firstChunk", trace.WithAttributes(
					attribute.Int64("ai.response.msToFirstChunk", ms),
				))
			}
			stepSpan.SetAttributes(
				attribute.Int64("ai.response.msToFinish", e.Performance.ResponseTimeMs),
			)
			// ai.response.avgOutputTokensPerSecond goes through selectAttributes
			// (sanitizeAttributeValue drops NaN) as a regular attribute, but
			// TS's addEvent call below bypasses selectAttributes entirely, so
			// the event attribute is NOT NaN-filtered — pass the raw value,
			// matching TS exactly.
			stepSpan.SetAttributes(setFiniteFloat64(nil, "ai.response.avgOutputTokensPerSecond", e.Performance.EffectiveOutputTokensPerSecond)...)
			stepSpan.AddEvent("ai.stream.finish", trace.WithAttributes(
				attribute.Int64("ai.response.msToFinish", e.Performance.ResponseTimeMs),
				attribute.Float64("ai.response.avgOutputTokensPerSecond", e.Performance.EffectiveOutputTokensPerSecond),
			))
		}
	}

	if e.ResponseID != "" {
		stepSpan.SetAttributes(
			attribute.String("ai.response.id", e.ResponseID),
			attribute.String("gen_ai.response.id", e.ResponseID),
		)
	}
	if e.ResponseModelID != "" {
		stepSpan.SetAttributes(attribute.String("ai.response.model", e.ResponseModelID))
	}
	if !e.ResponseTimestamp.IsZero() {
		stepSpan.SetAttributes(attribute.String("ai.response.timestamp", e.ResponseTimestamp.UTC().Format(time.RFC3339)))
	}
	if e.ProviderMetadata != nil {
		if b, err := json.Marshal(e.ProviderMetadata); err == nil {
			stepSpan.SetAttributes(attribute.String("ai.response.providerMetadata", string(b)))
		}
	}

	stepSpan.SetAttributes(attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}))
	if e.Usage.InputTokens != nil {
		stepSpan.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		stepSpan.SetAttributes(attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}

	if isLegacyObjectOperation(e.OperationType) {
		stepSpan.SetAttributes(legacyObjectUsageAttrs(e.Usage)...)
	} else {
		stepSpan.SetAttributes(legacyFullUsageAttrs(e.Usage)...)
	}

	stepSpan.End()
}

// OnStepError closes an in-flight step span when the provider call itself
// failed (doGenerate/doStream returned an error), so no OnStepEnd will ever
// fire for it — H4 item 2's "span leak on provider error" fix. Callers pass
// the most deeply-nested ctx available at the failure site (typically the
// ctx returned by FireOnLanguageModelCallStart), so ctx.Value(stepSpanKey{})
// still resolves via ctx's ancestor chain regardless of what other
// integrations layered on top of it afterward — unlike trace.SpanFromContext
// (used by OnError/OnEnd for the root span), a typed context key lookup is
// safe to call with a nested ctx without risk of touching another
// integration's span. Mirrors TS onError's `if (state.stepSpan) {
// recordSpanError(...); state.stepSpan.end(); }` when e.Error is set, or
// onAbort's `state.stepSpan.end()` (no error status) when it's nil.
func (i LegacyOpenTelemetry) OnStepError(ctx context.Context, e TelemetryErrorEvent) {
	stepSpan, ok := ctx.Value(stepSpanKey{}).(trace.Span)
	if !ok || !stepSpan.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(stepSpan, e.Error)
	}
	stepSpan.End()
}

// OnEnd sets output attributes on the root span and ends it. Dispatches on
// e.OperationType to reproduce TS's per-operation onEnd shape
// (onGenerateEnd / onObjectOperationEnd / onEmbedOperationEnd /
// onRerankOperationEnd in legacy-open-telemetry.ts), which differ
// materially: only generateText/streamText gets ai.response.text/reasoning/
// toolCalls/files and the full usage breakdown; generateObject/streamObject
// gets ai.response.object and a reduced usage set; embed/embedMany gets only
// ai.embedding(s); rerank gets nothing beyond finishReason (H3 follow-up 3).
// gen_ai.usage.* is never dual-emitted here (H1 already dropped
// gen_ai.system/gen_ai.request.model from the root span for the same
// reason: TS's onXEnd methods never set gen_ai.* on the root span at all).
func (i LegacyOpenTelemetry) OnEnd(ctx context.Context, e TelemetryFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs

	switch e.OperationType {
	case "ai.embed", "ai.embedMany":
		i.legacyOnEmbedOperationEnd(span, e, recordOutputs)
	case "ai.rerank":
		// TS onRerankOperationEnd sets nothing beyond ending the span.
	case "ai.generateObject", "ai.streamObject":
		i.legacyOnObjectOperationEnd(span, e, recordOutputs)
	default:
		i.legacyOnGenerateEnd(span, e, recordOutputs)
	}
	span.End()
}

// legacyOnGenerateEnd mirrors TS's onGenerateEnd (generateText/streamText).
func (i LegacyOpenTelemetry) legacyOnGenerateEnd(span trace.Span, e TelemetryFinishEvent, recordOutputs bool) {
	span.SetAttributes(attribute.String("ai.response.finishReason", e.FinishReason))
	if recordOutputs {
		if e.Text != "" {
			span.SetAttributes(attribute.String("ai.response.text", e.Text))
		}
		if e.Reasoning != "" {
			span.SetAttributes(attribute.String("ai.response.reasoning", e.Reasoning))
		}
		if s, ok := legacyToolCallsJSON(e.ToolCalls); ok {
			span.SetAttributes(attribute.String("ai.response.toolCalls", s))
		}
		if s, ok := legacyFilesJSON(e.Files); ok {
			span.SetAttributes(attribute.String("ai.response.files", s))
		}
	}
	if e.ProviderMetadata != nil {
		if b, err := json.Marshal(e.ProviderMetadata); err == nil {
			span.SetAttributes(attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
	span.SetAttributes(legacyFullUsageAttrs(e.Usage)...)
}

// legacyOnObjectOperationEnd mirrors TS's onObjectOperationEnd
// (generateObject/streamObject): ai.response.object instead of text, and the
// smaller legacyObjectUsageAttrs usage set.
func (i LegacyOpenTelemetry) legacyOnObjectOperationEnd(span trace.Span, e TelemetryFinishEvent, recordOutputs bool) {
	span.SetAttributes(attribute.String("ai.response.finishReason", e.FinishReason))
	if recordOutputs && e.Object != nil {
		if b, err := json.Marshal(e.Object); err == nil {
			span.SetAttributes(attribute.String("ai.response.object", string(b)))
		}
	}
	if e.ProviderMetadata != nil {
		if b, err := json.Marshal(e.ProviderMetadata); err == nil {
			span.SetAttributes(attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
	span.SetAttributes(legacyObjectUsageAttrs(e.Usage)...)
}

// legacyOnEmbedOperationEnd mirrors TS's onEmbedOperationEnd: ai.embedding
// (ai.embed) or ai.embeddings (ai.embedMany) output-gated, plus
// ai.usage.tokens which — like the nested doEmbed span's copy — is a plain
// value in TS (not {output: ...}), so it is set unconditionally (subject
// only to telemetry being enabled, not recordOutputs). No finishReason.
func (i LegacyOpenTelemetry) legacyOnEmbedOperationEnd(span trace.Span, e TelemetryFinishEvent, recordOutputs bool) {
	if e.Usage.TotalTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.tokens", *e.Usage.TotalTokens))
	}
	if !recordOutputs || e.Embedding == nil {
		return
	}
	switch v := e.Embedding.(type) {
	case [][]float64:
		if len(v) > 0 {
			span.SetAttributes(attribute.StringSlice("ai.embeddings", jsonStringifyEach(v)))
		}
	case []float64:
		if b, err := json.Marshal(v); err == nil {
			span.SetAttributes(attribute.String("ai.embedding", string(b)))
		}
	}
}

// OnFinish is a deprecated compatibility alias for OnEnd.
func (i LegacyOpenTelemetry) OnFinish(ctx context.Context, e TelemetryFinishEvent) {
	i.OnEnd(ctx, e)
}

// OnError records the error on the root span and ends it. It also
// defensively closes any nested "ai.evaluate.doEvaluate" span left open by
// OnEvaluationModelCallStart for this CallID, since
// OnEvaluationModelCallEnd is never notified on an error path (mirrors
// evaluate.ts, where onEvaluationModelCallEnd only fires on success).
func (i LegacyOpenTelemetry) OnError(ctx context.Context, e TelemetryErrorEvent) {
	if e.CallID != "" {
		if value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("evaluation", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				if e.Error != nil {
					RecordErrorOnSpan(entry.span, e.Error)
				}
				entry.span.End()
			}
		}
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(span, e.Error)
	}
	span.End()
}

// OnAbort closes any in-flight step span (mirroring TS onAbort's
// `state.stepSpan.end()`, no error status — LegacyOpenTelemetry has no
// inferenceSpan of its own to close, unlike GenAI's OpenTelemetry) and
// records the abort reason on the root span before ending it.
func (i LegacyOpenTelemetry) OnAbort(ctx context.Context, e TelemetryAbortEvent) {
	if stepSpan, ok := ctx.Value(stepSpanKey{}).(trace.Span); ok && stepSpan.IsRecording() {
		stepSpan.End()
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.End()
}

// ExecuteTool delegates directly to execute. Nested span support can be added here.
func (i LegacyOpenTelemetry) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}

// ---------------------------------------------------------------------------
// Registry — slice-based for composite fan-out (Gap 2)
// ---------------------------------------------------------------------------

var (
	mu           sync.RWMutex
	integrations []TelemetryIntegration
)

// RegisterTelemetryIntegration appends integrations to the global registry.
// Passing only NoopTelemetryIntegration{} resets to the quiet default for
// backward compatibility with older tests and examples.
// Safe to call concurrently with fire functions.
func RegisterTelemetryIntegration(integration TelemetryIntegration, more ...TelemetryIntegration) {
	mu.Lock()
	defer mu.Unlock()
	all := append([]TelemetryIntegration{integration}, more...)
	if len(all) == 1 {
		if _, ok := all[0].(NoopTelemetryIntegration); ok {
			integrations = all
			return
		}
		if len(integrations) == 1 {
			if _, ok := integrations[0].(NoopTelemetryIntegration); ok {
				integrations = nil
			}
		}
	}
	integrations = append(integrations, all...)
}

// AddTelemetryIntegration appends i to the list of registered integrations.
// All registered integrations receive every event (fan-out).
// Safe to call concurrently with fire functions.
func AddTelemetryIntegration(i TelemetryIntegration) {
	mu.Lock()
	defer mu.Unlock()
	integrations = append(integrations, i)
}

// ClearTelemetryIntegrations removes all registered integrations.
// After this call, telemetry events are silently discarded.
func ClearTelemetryIntegrations() {
	mu.Lock()
	defer mu.Unlock()
	integrations = nil
}

// GetTelemetryIntegration returns the first registered integration, or
// NoopTelemetryIntegration if none has been registered.
// Provided for backward compatibility; prefer the Fire* functions.
func GetTelemetryIntegration() TelemetryIntegration {
	mu.RLock()
	defer mu.RUnlock()
	if len(integrations) == 0 {
		return NoopTelemetryIntegration{}
	}
	return integrations[0]
}

// snapshot returns a copy of the integrations slice under read-lock.
func snapshot() []TelemetryIntegration {
	mu.RLock()
	defer mu.RUnlock()
	return append([]TelemetryIntegration(nil), integrations...)
}

func snapshotFor(settings *Settings) []TelemetryIntegration {
	if settings != nil && len(settings.Integrations) > 0 {
		return append([]TelemetryIntegration(nil), settings.Integrations...)
	}
	return snapshot()
}

func telemetryDisabled(settings *Settings) bool {
	return !Enabled(settings)
}

// ---------------------------------------------------------------------------
// Fire functions — fan-out to all registered integrations
// ---------------------------------------------------------------------------

// FireOnStart calls OnStart on every registered integration, threading the
// returned context through the chain so each integration can inject spans.
func FireOnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	PublishDiagnostic(ctx, DiagnosticEventOnStart, e)
	for _, i := range snapshotFor(e.Settings) {
		ctx = i.OnStart(ctx, e)
	}
	return ctx
}

// FireOnStepStart calls OnStepStart on every registered integration, threading
// the returned context through the chain so each integration can inject step spans.
func FireOnStepStart(ctx context.Context, e TelemetryStepStartEvent) context.Context {
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	PublishDiagnostic(ctx, DiagnosticEventOnStepStart, e)
	for _, i := range snapshotFor(e.Settings) {
		ctx = i.OnStepStart(ctx, e)
	}
	return ctx
}

// FireOnLanguageModelCallStart publishes and fans out a model-call start event.
// FireOnLanguageModelCallStart calls OnLanguageModelCallStart on every
// registered integration that implements it, threading ctx through each call
// so an integration (e.g. the OTel one) can embed a model-call span in the
// returned ctx. The caller should use the returned ctx for the actual
// provider call (594029e), so provider HTTP spans become children of it.
func FireOnLanguageModelCallStart(ctx context.Context, e LanguageModelCallStartEvent) context.Context {
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	PublishDiagnostic(ctx, DiagnosticEventOnLanguageModelCallStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(languageModelCallStartHandler); ok {
			ctx = handler.OnLanguageModelCallStart(ctx, e)
		}
	}
	return ctx
}

// FireOnLanguageModelCallEnd publishes and fans out a model-call end event.
func FireOnLanguageModelCallEnd(ctx context.Context, e LanguageModelCallEndEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnLanguageModelCallEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(languageModelCallEndHandler); ok {
			handler.OnLanguageModelCallEnd(ctx, e)
		}
	}
}

// FireOnEmbedStart publishes and fans out an embedding model-call start event.
func FireOnEmbedStart(ctx context.Context, e EmbeddingModelCallStartEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEmbedStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(embedStartHandler); ok {
			handler.OnEmbedStart(ctx, e)
		}
	}
}

// FireOnEmbedEnd publishes and fans out an embedding model-call end event.
func FireOnEmbedEnd(ctx context.Context, e EmbeddingModelCallEndEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEmbedEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(embedEndHandler); ok {
			handler.OnEmbedEnd(ctx, e)
			continue
		}
		if handler, ok := integration.(embedFinishHandler); ok {
			handler.OnEmbedFinish(ctx, e)
		}
	}
}

// FireOnEmbedFinish publishes and fans out an embedding model-call finish event.
//
// Deprecated: use FireOnEmbedEnd.
func FireOnEmbedFinish(ctx context.Context, e EmbeddingModelCallEndEvent) {
	FireOnEmbedEnd(ctx, e)
}

// FireOnRerankStart publishes and fans out a reranking model-call start event.
func FireOnRerankStart(ctx context.Context, e RerankingModelCallStartEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnRerankStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(rerankStartHandler); ok {
			handler.OnRerankStart(ctx, e)
		}
	}
}

// FireOnRerankEnd publishes and fans out a reranking model-call end event.
func FireOnRerankEnd(ctx context.Context, e RerankingModelCallEndEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnRerankEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(rerankEndHandler); ok {
			handler.OnRerankEnd(ctx, e)
			continue
		}
		if handler, ok := integration.(rerankFinishHandler); ok {
			handler.OnRerankFinish(ctx, e)
		}
	}
}

// FireOnRerankFinish publishes and fans out a reranking model-call finish event.
//
// Deprecated: use FireOnRerankEnd.
func FireOnRerankFinish(ctx context.Context, e RerankingModelCallEndEvent) {
	FireOnRerankEnd(ctx, e)
}

// FireOnToolCallStart calls OnToolExecutionStart on every registered integration,
// threading the returned context through the chain.
func FireOnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	PublishDiagnostic(ctx, DiagnosticEventOnToolExecutionStart, e)
	for _, i := range snapshotFor(e.Settings) {
		ctx = i.OnToolExecutionStart(ctx, e)
	}
	return ctx
}

// FireOnToolCallFinish calls OnToolExecutionEnd on every registered integration.
func FireOnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnToolExecutionEnd, e)
	for _, i := range snapshotFor(e.Settings) {
		i.OnToolExecutionEnd(ctx, e)
	}
}

// FireOnChunk is retained for source compatibility with older integrations.
// The current TypeScript AI SDK no longer emits telemetry chunk events, so this
// function intentionally does not publish diagnostics or call integrations.
//
// Deprecated: chunk telemetry has been removed.
func FireOnChunk(ctx context.Context, e TelemetryChunkEvent) {
}

// FireOnStepFinish emits a step-end event.
//
// Deprecated: use FireOnStepEnd.
func FireOnStepFinish(ctx context.Context, e TelemetryStepFinishEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnStepEnd, e)
	PublishDiagnostic(ctx, DiagnosticEventOnStepFinish, e)
	for _, i := range snapshotFor(e.Settings) {
		i.OnStepEnd(ctx, e)
		if handler, ok := i.(deprecatedStepFinishHandler); ok {
			handler.OnStepFinish(ctx, e)
		}
	}
}

// FireOnStepEnd calls OnStepEnd on every registered integration.
func FireOnStepEnd(ctx context.Context, e TelemetryStepEndEvent) {
	FireOnStepFinish(ctx, e)
}

// FireOnEnd calls OnEnd on every registered integration, falling back to the
// deprecated OnFinish method for older integrations.
func FireOnEnd(ctx context.Context, e TelemetryFinishEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEnd, e)
	for _, i := range snapshotFor(e.Settings) {
		if handler, ok := i.(endHandler); ok {
			handler.OnEnd(ctx, e)
			continue
		}
		i.OnFinish(ctx, e)
	}
}

// FireOnFinish calls OnFinish on every registered integration.
//
// Deprecated: use FireOnEnd.
func FireOnFinish(ctx context.Context, e TelemetryFinishEvent) {
	FireOnEnd(ctx, e)
}

// FireOnError calls OnError on every registered integration.
func FireOnError(ctx context.Context, e TelemetryErrorEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnError, e)
	for _, i := range snapshotFor(e.Settings) {
		i.OnError(ctx, e)
	}
}

// FireOnAbort calls OnAbort on every registered integration.
func FireOnAbort(ctx context.Context, e TelemetryAbortEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnAbort, e)
	for _, i := range snapshotFor(e.Settings) {
		if h, ok := i.(abortHandler); ok {
			h.OnAbort(ctx, e)
		}
	}
}

// FireOnStepError closes a still-open step span (and, for the GenAI
// integration, the nested model-call/"chat" span) when the provider call
// itself failed, so neither OnStepEnd nor OnLanguageModelCallEnd will ever
// fire for it (H4 item 2: "span leak on provider error"). Callers at the
// generateText/streamText/generateObject/streamObject provider-call failure
// sites pass the most deeply-nested ctx they have available (typically the
// ctx returned by FireOnLanguageModelCallStart) — unlike FireOnError/
// FireOnAbort's root-span handling (which keys off trace.SpanFromContext and
// is only ever called with the outer, unnested ctx), integrations implement
// OnStepError using their own private context key / CallID-keyed lookups, so
// it is safe to call from any ctx without risking closing another
// integration's span. Set e.Error to record an error status on the closed
// span (provider error); leave it nil to just close it without an error
// status (abort).
func FireOnStepError(ctx context.Context, e TelemetryErrorEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	for _, i := range snapshotFor(e.Settings) {
		if h, ok := i.(stepErrorHandler); ok {
			h.OnStepError(ctx, e)
		}
	}
}

// FireExecuteTool chains ExecuteTool across all registered integrations (Gap 4).
// Each integration wraps the next; the actual tool function is at the innermost level.
func FireExecuteTool(
	ctx context.Context,
	toolName string,
	args map[string]interface{},
	execute func(ctx context.Context, args map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return FireExecuteToolWithSettings(ctx, nil, toolName, args, execute)
}

// FireExecuteToolWithSettings chains ExecuteTool across resolved integrations.
func FireExecuteToolWithSettings(
	ctx context.Context,
	settings *Settings,
	toolName string,
	args map[string]interface{},
	execute func(ctx context.Context, args map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	if telemetryDisabled(settings) {
		return execute(ctx, args)
	}
	is := snapshotFor(settings)
	if len(is) == 0 {
		return execute(ctx, args)
	}
	// Build chain from innermost (execute) outward.
	fn := execute
	for i := len(is) - 1; i >= 0; i-- {
		fn = makeToolFn(is[i], toolName, fn)
	}
	return fn(ctx, args)
}

// makeToolFn avoids loop-variable capture issues when building the ExecuteTool chain.
func makeToolFn(
	integration TelemetryIntegration,
	toolName string,
	next func(context.Context, map[string]interface{}) (interface{}, error),
) func(context.Context, map[string]interface{}) (interface{}, error) {
	return func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		return integration.ExecuteTool(ctx, toolName, args, next)
	}
}

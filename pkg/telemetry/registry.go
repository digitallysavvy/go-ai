package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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
	// Prompt and System are only populated when Settings.RecordInputs is true.
	Prompt string
	System string
	// ValueCount is populated for batch value operations such as embedMany.
	ValueCount int
	// RuntimeContext and ToolsContext contain only keys explicitly included by
	// Settings.IncludeRuntimeContext and Settings.IncludeToolsContext.
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
}

// TelemetryStepStartEvent is passed to TelemetryIntegration.OnStepStart.
type TelemetryStepStartEvent struct {
	Settings *Settings
	// OperationType is the canonical AI operation name, e.g. "ai.generateText".
	// Used to name the per-step OTel child span.
	OperationType  string
	StepNumber     int
	ModelProvider  string
	ModelID        string
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
}

// LanguageModelCallStartEvent is emitted immediately before a provider model call.
type LanguageModelCallStartEvent struct {
	Settings      *Settings
	CallID        string
	ModelProvider string
	ModelID       string
	Prompt        interface{}
	Tools         interface{}
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
}

// LanguageModelCallPerformance contains timing statistics for provider model work.
type LanguageModelCallPerformance struct {
	ResponseTimeMs                 int64    `json:"responseTimeMs"`
	EffectiveOutputTokensPerSecond float64  `json:"effectiveOutputTokensPerSecond"`
	OutputTokensPerSecond          *float64 `json:"outputTokensPerSecond,omitempty"`
	InputTokensPerSecond           *float64 `json:"inputTokensPerSecond,omitempty"`
	EffectiveTotalTokensPerSecond  float64  `json:"effectiveTotalTokensPerSecond"`
	TimeToFirstOutputTokenMs       *int64   `json:"timeToFirstOutputTokenMs,omitempty"`
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

// TelemetryStepFinishEvent is passed to TelemetryIntegration.OnStepFinish.
type TelemetryStepFinishEvent struct {
	StepNumber   int
	FinishReason string
	Usage        TelemetryUsage

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

	// Settings holds the caller-supplied telemetry configuration.
	Settings       *Settings
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
}

// TelemetryFinishEvent is passed to TelemetryIntegration.OnFinish.
type TelemetryFinishEvent struct {
	FinishReason  string
	Usage         TelemetryUsage
	ModelProvider string
	ModelID       string
	// Text is the full generated text. Integrations should check
	// Settings.RecordOutputs before recording this value.
	Text string
	// Files holds any model-generated output files (e.g. images, audio).
	// Integrations should check Settings.RecordOutputs before recording file data.
	Files          []types.GeneratedFileContent
	Settings       *Settings
	RuntimeContext map[string]interface{}
	ToolsContext   map[string]interface{}
}

// TelemetryErrorEvent is passed to TelemetryIntegration.OnError.
type TelemetryErrorEvent struct {
	Settings *Settings
	Error    error
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
	// returned context so OnStepFinish can retrieve and end it.
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
	// Deprecated: use OnStepFinish, OnLanguageModelCallEnd, and OnEnd.
	OnChunk(ctx context.Context, e TelemetryChunkEvent)

	// OnStepFinish is called after each LLM step completes.
	OnStepFinish(ctx context.Context, e TelemetryStepFinishEvent)

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
	OnLanguageModelCallStart(context.Context, LanguageModelCallStartEvent)
}

type languageModelCallEndHandler interface {
	OnLanguageModelCallEnd(context.Context, LanguageModelCallEndEvent)
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
func (NoopTelemetryIntegration) OnChunk(_ context.Context, _ TelemetryChunkEvent)           {}
func (NoopTelemetryIntegration) OnStepFinish(_ context.Context, _ TelemetryStepFinishEvent) {}
func (NoopTelemetryIntegration) OnFinish(_ context.Context, _ TelemetryFinishEvent)         {}
func (NoopTelemetryIntegration) OnError(_ context.Context, _ TelemetryErrorEvent)           {}
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

// OTelTelemetryIntegration translates TelemetryIntegration events into
// OpenTelemetry spans.  Register it to enable OTel tracing:
//
//	telemetry.RegisterTelemetryIntegration(telemetry.OTelTelemetryIntegration{})
type OTelTelemetryIntegration struct{}

type otelSpanEntry struct {
	span trace.Span
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

func customSpanAttributes(ctx context.Context, settings *Settings, opts EnrichSpanOptions) []attribute.KeyValue {
	if settings == nil || settings.EnrichSpan == nil {
		return nil
	}
	defer func() {
		_ = recover()
	}()
	attrs := settings.EnrichSpan(ctx, opts)
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
			out = append(out, attribute.Float64(key, v))
		case []string:
			out = append(out, attribute.StringSlice(key, v))
		case []bool:
			out = append(out, attribute.BoolSlice(key, v))
		case []int:
			out = append(out, attribute.IntSlice(key, v))
		case []int64:
			out = append(out, attribute.Int64Slice(key, v))
		case []float64:
			out = append(out, attribute.Float64Slice(key, v))
		default:
			if b, err := json.Marshal(v); err == nil {
				out = append(out, attribute.String(key, string(b)))
			}
		}
	}
	return out
}

// OnStart starts the root OTel span and embeds it in the returned context.
// Returns ctx unchanged when settings explicitly disables telemetry.
func (OTelTelemetryIntegration) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := GetTracer(e.Settings)
	spanName := e.OperationType
	if e.Settings != nil && e.Settings.FunctionID != "" {
		spanName += "." + e.Settings.FunctionID
	}
	ctx, span := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationType,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("ai.operationId", e.OperationType),
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if e.Settings != nil && e.Settings.FunctionID != "" {
		span.SetAttributes(attribute.String("ai.telemetry.functionId", e.Settings.FunctionID))
	}
	if (e.Settings == nil || e.Settings.RecordInputs) && e.Prompt != "" {
		span.SetAttributes(attribute.String("ai.prompt", e.Prompt))
		if e.OperationType == "ai.embed" {
			span.SetAttributes(attribute.String("ai.value", e.Prompt))
		}
	}
	if e.OperationType == "ai.embedMany" && e.ValueCount > 0 {
		span.SetAttributes(attribute.Int("ai.values.count", e.ValueCount))
	}
	return ctx // span is embedded via OTel context propagation
}

// stepSpanKey is a private context key used to pass the OTel step span from
// OnStepStart to OnStepFinish without relying on trace.SpanFromContext (which
// would return the innermost span, potentially set by provider-level tracing).
type stepSpanKey struct{}

// OnStepStart creates a child OTel span for the step and embeds it in the
// returned context via stepSpanKey, mirroring the TS SDK's onStepStart span.
func (OTelTelemetryIntegration) OnStepStart(ctx context.Context, e TelemetryStepStartEvent) context.Context {
	rootSpan := trace.SpanFromContext(ctx)
	if !rootSpan.IsRecording() {
		return ctx
	}
	tracer := rootSpan.TracerProvider().Tracer("go-ai")
	opType := e.OperationType
	if opType == "" {
		opType = "ai.step"
	}
	spanName := fmt.Sprintf("%s step %d", opType, e.StepNumber)
	ctx, stepSpan := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, e.Settings, EnrichSpanOptions{
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
	return context.WithValue(ctx, stepSpanKey{}, stepSpan)
}

// OnLanguageModelCallStart creates a child span for provider model inference.
func (OTelTelemetryIntegration) OnLanguageModelCallStart(ctx context.Context, e LanguageModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := "chat"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeLanguageModel,
		OperationType: "ai.generateText",
		CallID:        e.CallID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if e.CallID != "" {
		otelModelCallSpans.Store(otelSpanKey("languageModel", e.CallID), otelSpanEntry{span: span})
	}
}

// OnLanguageModelCallEnd records model-call attributes and ends the inference span.
func (OTelTelemetryIntegration) OnLanguageModelCallEnd(_ context.Context, e LanguageModelCallEndEvent) {
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("languageModel", e.CallID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	entry.span.SetAttributes(
		attribute.String("ai.response.finishReason", e.FinishReason),
		attribute.Int64("ai.response.responseTimeMs", e.Performance.ResponseTimeMs),
		attribute.Float64("ai.response.effectiveOutputTokensPerSecond", e.Performance.EffectiveOutputTokensPerSecond),
		attribute.Float64("ai.response.effectiveTotalTokensPerSecond", e.Performance.EffectiveTotalTokensPerSecond),
	)
	if e.Performance.OutputTokensPerSecond != nil {
		entry.span.SetAttributes(attribute.Float64("ai.response.outputTokensPerSecond", *e.Performance.OutputTokensPerSecond))
	}
	if e.Performance.InputTokensPerSecond != nil {
		entry.span.SetAttributes(attribute.Float64("ai.response.inputTokensPerSecond", *e.Performance.InputTokensPerSecond))
	}
	if e.Performance.TimeToFirstOutputTokenMs != nil {
		entry.span.SetAttributes(attribute.Int64("ai.response.timeToFirstOutputTokenMs", *e.Performance.TimeToFirstOutputTokenMs))
	}
	if e.Usage.InputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}
	entry.span.End()
}

// OnEmbedStart creates a child span for embedding model inference.
func (OTelTelemetryIntegration) OnEmbedStart(ctx context.Context, e EmbeddingModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := "embeddings"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeEmbedding,
		OperationType: e.OperationID,
		CallID:        callID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "embeddings"),
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if callID != "" {
		otelModelCallSpans.Store(otelSpanKey("embedding", callID), otelSpanEntry{span: span})
	}
}

// OnEmbedEnd records embedding attributes and ends the embedding span.
func (OTelTelemetryIntegration) OnEmbedEnd(_ context.Context, e EmbeddingModelCallEndEvent) {
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("embedding", callID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	entry.span.SetAttributes(attribute.Int("ai.embeddings.count", len(e.Embeddings)))
	if e.Usage.InputTokens > 0 {
		entry.span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", e.Usage.InputTokens))
	}
	entry.span.End()
}

// OnRerankStart creates a child span for reranking model inference.
func (OTelTelemetryIntegration) OnRerankStart(ctx context.Context, e RerankingModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.CallID, e.OperationID)
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := "reranking"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(ctx, spanName)
	if attrs := customSpanAttributes(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeReranking,
		OperationType: e.OperationID,
		CallID:        callID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "reranking"),
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if callID != "" {
		otelModelCallSpans.Store(otelSpanKey("reranking", callID), otelSpanEntry{span: span})
	}
}

// OnRerankEnd records reranking attributes and ends the reranking span.
func (OTelTelemetryIntegration) OnRerankEnd(_ context.Context, e RerankingModelCallEndEvent) {
	callID := modelCallID(e.CallID, e.OperationID)
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("reranking", callID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	entry.span.SetAttributes(attribute.Int("ai.reranking.results.count", len(e.Ranking)))
	entry.span.End()
}

// OnToolExecutionStart starts a child span for tool execution and embeds it.
func (OTelTelemetryIntegration) OnToolExecutionStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx
	}
	tracer := span.TracerProvider().Tracer("go-ai")
	ctx, child := tracer.Start(ctx, "ai.toolCall."+e.ToolName)
	if attrs := customSpanAttributes(ctx, e.Settings, EnrichSpanOptions{
		SpanType: SpanTypeTool,
		CallID:   e.ToolCallID,
	}); len(attrs) > 0 {
		child.SetAttributes(attrs...)
	}
	child.SetAttributes(
		attribute.String("ai.toolCall.id", e.ToolCallID),
		attribute.String("ai.toolCall.name", e.ToolName),
	)
	return ctx
}

// OnToolExecutionEnd ends the tool execution child span.
func (OTelTelemetryIntegration) OnToolExecutionEnd(ctx context.Context, e TelemetryToolCallFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.Int64("ai.toolCall.durationMs", e.DurationMs))
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End()
}

// OnToolCallStart is the previous Go name for OnToolExecutionStart.
//
// Deprecated: use OnToolExecutionStart.
func (i OTelTelemetryIntegration) OnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	return i.OnToolExecutionStart(ctx, e)
}

// OnToolCallFinish is the previous Go name for OnToolExecutionEnd.
//
// Deprecated: use OnToolExecutionEnd.
func (i OTelTelemetryIntegration) OnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent) {
	i.OnToolExecutionEnd(ctx, e)
}

func (OTelTelemetryIntegration) OnChunk(_ context.Context, _ TelemetryChunkEvent) {}

// OnStepFinish records step-level OTel attributes on the child step span created
// by OnStepStart and ends the span. Mirrors the TS SDK's onStepFinish behavior.
func (OTelTelemetryIntegration) OnStepFinish(ctx context.Context, e TelemetryStepFinishEvent) {
	stepSpan, ok := ctx.Value(stepSpanKey{}).(trace.Span)
	if !ok || !stepSpan.IsRecording() {
		return
	}
	recordOutputs := e.Settings != nil && e.Settings.RecordOutputs

	stepSpan.SetAttributes(attribute.String("ai.response.finishReason", e.FinishReason))

	if recordOutputs && e.Text != "" {
		stepSpan.SetAttributes(attribute.String("ai.response.text", e.Text))
	}
	if recordOutputs && e.Reasoning != "" {
		stepSpan.SetAttributes(attribute.String("ai.response.reasoning", e.Reasoning))
	}
	if recordOutputs && len(e.ToolCalls) > 0 {
		type toolCallEntry struct {
			ToolCallID string      `json:"toolCallId"`
			ToolName   string      `json:"toolName"`
			Input      interface{} `json:"input"`
		}
		entries := make([]toolCallEntry, len(e.ToolCalls))
		for i, tc := range e.ToolCalls {
			entries[i] = toolCallEntry{
				ToolCallID: tc.ID,
				ToolName:   tc.ToolName,
				Input:      tc.Arguments,
			}
		}
		if b, err := json.Marshal(entries); err == nil {
			stepSpan.SetAttributes(attribute.String("ai.response.toolCalls", string(b)))
		}
	}
	if recordOutputs && len(e.Files) > 0 {
		type fileEntry struct {
			Type      string `json:"type"`
			MediaType string `json:"mediaType"`
			Data      string `json:"data"`
		}
		entries := make([]fileEntry, len(e.Files))
		for i, f := range e.Files {
			entries[i] = fileEntry{
				Type:      "file",
				MediaType: f.MediaType,
				Data:      base64.StdEncoding.EncodeToString(f.Data),
			}
		}
		if b, err := json.Marshal(entries); err == nil {
			stepSpan.SetAttributes(attribute.String("ai.response.files", string(b)))
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
		stepSpan.SetAttributes(
			attribute.Int64("ai.usage.inputTokens", *e.Usage.InputTokens),
			attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens),
		)
	}
	if e.Usage.OutputTokens != nil {
		stepSpan.SetAttributes(
			attribute.Int64("ai.usage.outputTokens", *e.Usage.OutputTokens),
			attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens),
		)
	}
	if e.Usage.TotalTokens != nil {
		stepSpan.SetAttributes(attribute.Int64("ai.usage.totalTokens", *e.Usage.TotalTokens))
	}
	if e.Usage.ReasoningTokens != nil {
		stepSpan.SetAttributes(
			attribute.Int64("ai.usage.reasoningTokens", *e.Usage.ReasoningTokens),
			attribute.Int64("ai.usage.outputTokenDetails.reasoningTokens", *e.Usage.ReasoningTokens),
		)
	}
	if e.Usage.CacheReadInputTokens != nil {
		stepSpan.SetAttributes(
			attribute.Int64("ai.usage.cachedInputTokens", *e.Usage.CacheReadInputTokens),
			attribute.Int64("ai.usage.inputTokenDetails.cacheReadTokens", *e.Usage.CacheReadInputTokens),
		)
	}
	if e.Usage.CacheCreationInputTokens != nil {
		stepSpan.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.cacheWriteTokens", *e.Usage.CacheCreationInputTokens))
	}
	if e.Usage.NoCacheInputTokens != nil {
		stepSpan.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.noCacheTokens", *e.Usage.NoCacheInputTokens))
	}
	if e.Usage.OutputTextTokens != nil {
		stepSpan.SetAttributes(attribute.Int64("ai.usage.outputTokenDetails.textTokens", *e.Usage.OutputTextTokens))
	}

	stepSpan.End()
}

// OnEnd sets output attributes on the root span and ends it.
func (OTelTelemetryIntegration) OnEnd(ctx context.Context, e TelemetryFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Settings != nil && e.Settings.RecordOutputs && e.Text != "" {
		span.SetAttributes(attribute.String("ai.response.text", e.Text))
	}
	if e.Settings != nil && e.Settings.RecordOutputs && len(e.Files) > 0 {
		type fileEntry struct {
			Type      string `json:"type"`
			MediaType string `json:"mediaType"`
			Data      string `json:"data"`
		}
		entries := make([]fileEntry, len(e.Files))
		for i, f := range e.Files {
			entries[i] = fileEntry{
				Type:      "file",
				MediaType: f.MediaType,
				Data:      base64.StdEncoding.EncodeToString(f.Data),
			}
		}
		if b, err := json.Marshal(entries); err == nil {
			span.SetAttributes(attribute.String("ai.response.files", string(b)))
		}
	}
	span.SetAttributes(attribute.String("ai.response.finishReason", e.FinishReason))
	if e.ModelProvider != "" {
		span.SetAttributes(attribute.String("gen_ai.system", e.ModelProvider))
	}
	if e.ModelID != "" {
		span.SetAttributes(attribute.String("gen_ai.request.model", e.ModelID))
	}
	// Gen AI semantic convention attributes (OpenTelemetry Gen AI spec).
	if e.Usage.InputTokens != nil {
		span.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		span.SetAttributes(attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}

	// Legacy ai.usage.* attributes — TS SDK emits both namespaces for backward compat.
	if e.Usage.InputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.outputTokens", *e.Usage.OutputTokens))
	}
	if e.Usage.TotalTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.totalTokens", *e.Usage.TotalTokens))
		if e.Usage.InputTokens == nil && e.Usage.OutputTokens == nil {
			span.SetAttributes(attribute.Int64("ai.usage.tokens", *e.Usage.TotalTokens))
		}
	}
	if e.Usage.ReasoningTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.reasoningTokens", *e.Usage.ReasoningTokens))
	}
	// ai.usage.cachedInputTokens is a legacy flat alias for cacheReadTokens.
	if e.Usage.CacheReadInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.cachedInputTokens", *e.Usage.CacheReadInputTokens))
	}
	if e.Usage.NoCacheInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.noCacheTokens", *e.Usage.NoCacheInputTokens))
	}
	if e.Usage.CacheReadInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.cacheReadTokens", *e.Usage.CacheReadInputTokens))
	}
	if e.Usage.CacheCreationInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.cacheWriteTokens", *e.Usage.CacheCreationInputTokens))
	}
	if e.Usage.OutputTextTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.outputTokenDetails.textTokens", *e.Usage.OutputTextTokens))
	}
	if e.Usage.ReasoningTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.outputTokenDetails.reasoningTokens", *e.Usage.ReasoningTokens))
	}
	span.End()
}

// OnFinish is a deprecated compatibility alias for OnEnd.
func (i OTelTelemetryIntegration) OnFinish(ctx context.Context, e TelemetryFinishEvent) {
	i.OnEnd(ctx, e)
}

// OnError records the error on the root span and ends it.
func (OTelTelemetryIntegration) OnError(ctx context.Context, e TelemetryErrorEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End()
}

// ExecuteTool delegates directly to execute. Nested span support can be added here.
func (OTelTelemetryIntegration) ExecuteTool(
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
func FireOnLanguageModelCallStart(ctx context.Context, e LanguageModelCallStartEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnLanguageModelCallStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(languageModelCallStartHandler); ok {
			handler.OnLanguageModelCallStart(ctx, e)
		}
	}
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

// FireOnStepFinish calls OnStepFinish on every registered integration.
func FireOnStepFinish(ctx context.Context, e TelemetryStepFinishEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnStepFinish, e)
	for _, i := range snapshotFor(e.Settings) {
		i.OnStepFinish(ctx, e)
	}
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

package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ---------------------------------------------------------------------------
// mapProviderName / mapOperationName (otel/src/gen-ai-format-messages.ts)
// ---------------------------------------------------------------------------

type providerNameMapping struct{ prefix, mapped string }

// wellKnownProviderPrefixes is checked longest-prefix-first (declaration
// order matters for multi-segment prefixes like "google.vertex" needing to
// win over the single-segment "google"), mirroring TS mapProviderName.
var wellKnownProviderPrefixes = []providerNameMapping{
	{"google.vertex", "gcp.vertex_ai"},
	{"google.generative-ai", "gcp.gemini"},
	{"google-vertex", "gcp.vertex_ai"},
	{"amazon-bedrock", "aws.bedrock"},
	{"azure-openai", "azure.ai.openai"},
	{"anthropic", "anthropic"},
	{"openai", "openai"},
	{"azure", "azure.ai.inference"},
	{"google", "gcp.gemini"},
	{"mistral", "mistral_ai"},
	{"cohere", "cohere"},
	{"bedrock", "aws.bedrock"},
	{"groq", "groq"},
	{"deepseek", "deepseek"},
	{"perplexity", "perplexity"},
	{"xai", "x_ai"},
}

// mapProviderName maps a go-ai provider string to a well-known
// gen_ai.provider.name value per the OTel GenAI SemConv, mirroring TS's
// mapProviderName (eb70e72, fc15550).
func mapProviderName(provider string) string {
	lower := strings.ToLower(provider)
	for _, m := range wellKnownProviderPrefixes {
		if lower == m.prefix || strings.HasPrefix(lower, m.prefix+".") || strings.HasPrefix(lower, m.prefix+"-") {
			return m.mapped
		}
	}
	return provider
}

var operationNameMapping = map[string]string{
	"ai.generateText":   "invoke_agent",
	"ai.streamText":     "invoke_agent",
	"ai.generateObject": "invoke_agent",
	"ai.streamObject":   "invoke_agent",
	"ai.embed":          "embeddings",
	"ai.embedMany":      "embeddings",
	"ai.rerank":         "rerank",
}

// mapOperationName maps a go-ai operationId to a gen_ai.operation.name value,
// mirroring TS's mapOperationName (fc15550).
func mapOperationName(operationID string) string {
	if v, ok := operationNameMapping[operationID]; ok {
		return v
	}
	return operationID
}

// ---------------------------------------------------------------------------
// OpenTelemetry: GenAI semantic-convention integration
// ---------------------------------------------------------------------------

// OpenTelemetryOptions configures an OpenTelemetry (GenAI semconv)
// integration, matching TS's `new OpenTelemetry(options)`
// (otel/src/open-telemetry.ts).
type OpenTelemetryOptions struct {
	// Tracer is the OTel tracer to use. Defaults to the global tracer
	// provider's "ai-sdk" tracer when nil.
	Tracer trace.Tracer
	// EnrichSpan adds custom attributes to spans as they are created.
	// SDK-managed attributes win on key collisions. A per-call
	// Settings.EnrichSpan, when set, overrides this constructor-level value.
	EnrichSpan EnrichSpanFunc

	// The following gate emission of AI-SDK-specific supplemental
	// attributes that are not part of the GenAI SemConv (18651f6). All
	// default to false (off) so a GenAI backend sees only standard
	// gen_ai.* attributes unless the caller opts in.

	// Usage emits legacy ai.usage.* attributes alongside gen_ai.usage.*.
	Usage bool
	// ProviderMetadata emits ai.response.providerMetadata as JSON.
	ProviderMetadata bool
	// Embedding emits ai.value/ai.values/ai.embedding/ai.embeddings.
	Embedding bool
	// Reranking emits ai.documents/ai.ranking.
	Reranking bool
	// ExperimentalEvaluation emits ai.evaluation.* attributes.
	ExperimentalEvaluation bool
	// RuntimeContext emits flattened ai.settings.context.* attributes.
	RuntimeContext bool
	// Headers emits ai.request.headers.* attributes.
	Headers bool
	// ToolChoice emits ai.prompt.toolChoice on step spans.
	ToolChoice bool
	// Schema emits ai.schema/ai.schema.name/ai.schema.description.
	Schema bool
}

// OpenTelemetry translates TelemetryIntegration events into OpenTelemetry
// GenAI semantic-convention spans (gen_ai.* attributes), matching TS's
// `otel/src/open-telemetry.ts` OpenTelemetry class (fc15550, 8284dfa).
// Unimplemented optional lifecycle methods fall through to
// NoopTelemetryIntegration via embedding.
//
// The zero value is valid and uses the global OTel tracer provider with all
// supplemental attributes off, matching TS's `new OpenTelemetry()`.
type OpenTelemetry struct {
	NoopTelemetryIntegration
	tracer     trace.Tracer
	enrichSpan EnrichSpanFunc
	opts       OpenTelemetryOptions
}

// NewOpenTelemetry creates a GenAI semconv OpenTelemetry integration.
func NewOpenTelemetry(opts OpenTelemetryOptions) OpenTelemetry {
	return OpenTelemetry{tracer: opts.Tracer, enrichSpan: opts.EnrichSpan, opts: opts}
}

func (i OpenTelemetry) tracerFor(settings *Settings) trace.Tracer {
	if !Enabled(settings) {
		return noopTracer()
	}
	if i.tracer != nil {
		return i.tracer
	}
	return GetTracer(settings)
}

func (i OpenTelemetry) customAttrs(ctx context.Context, settings *Settings, opts EnrichSpanOptions) []attribute.KeyValue {
	return customSpanAttributes(ctx, i.enrichSpan, settings, opts)
}

// genAI span-tracking state, kept separate from LegacyOpenTelemetry's own
// otelModelCallSpans/stepSpanKey so the two integrations never collide when
// both are registered at once.
var genAICallSpans sync.Map // map[string]otelSpanEntry, keyed by otelSpanKey(kind, callID)

type genAIStepSpanKey struct{}
type genAIRuntimeContextKey struct{}

func genAISpanKey(kind, callID string) string { return "genai:" + kind + ":" + callID }

// msToSeconds converts a millisecond duration to seconds, matching TS's
// msToSeconds helper used for gen_ai.client.operation.* attributes.
func msToSeconds(ms int64) float64 { return float64(ms) / 1000 }

// OnStart starts the root operation span with GenAI + AI-SDK-specific
// attributes and embeds it (and, when enabled, flattened runtime context
// attrs for reuse by descendant spans) in the returned context.
func (i OpenTelemetry) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := i.tracerFor(e.Settings)
	spanName := e.OperationType
	if e.Settings != nil && e.Settings.FunctionID != "" {
		spanName += " " + e.Settings.FunctionID
	}
	ctx, span := tracer.Start(ctx, spanName)
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationType,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", mapOperationName(e.OperationType)),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if e.Settings != nil && e.Settings.FunctionID != "" {
		span.SetAttributes(attribute.String("gen_ai.agent.name", e.Settings.FunctionID))
	}
	if (e.Settings == nil || e.Settings.RecordInputs) && e.Prompt != "" {
		span.SetAttributes(attribute.String("gen_ai.system_instructions", e.Prompt))
	}
	if i.opts.Embedding && e.OperationType == "ai.embedMany" && e.ValueCount > 0 {
		span.SetAttributes(attribute.Int("ai.values.count", e.ValueCount))
	}
	if i.opts.RuntimeContext {
		if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
			span.SetAttributes(attrs...)
			ctx = context.WithValue(ctx, genAIRuntimeContextKey{}, attrs)
		}
	}
	return ctx
}

// OnStepStart creates a "step {n}" child span with gen_ai.operation.name
// "agent_step", matching TS's step-level span naming (152c67c).
func (i OpenTelemetry) OnStepStart(ctx context.Context, e TelemetryStepStartEvent) context.Context {
	rootSpan := trace.SpanFromContext(ctx)
	if !rootSpan.IsRecording() {
		return ctx
	}
	tracer := rootSpan.TracerProvider().Tracer("go-ai")
	spanName := "step " + itoa(e.StepNumber)
	ctx, stepSpan := tracer.Start(ctx, spanName)
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeStep,
		OperationType:  e.OperationType,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		stepSpan.SetAttributes(attrs...)
	}
	stepSpan.SetAttributes(
		attribute.String("gen_ai.operation.name", "agent_step"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	return context.WithValue(ctx, genAIStepSpanKey{}, stepSpan)
}

// OnLanguageModelCallStart creates the "chat" model-call span with GenAI
// request parameters and returns it embedded in ctx (594029e), so the
// provider call (and any HTTP spans it creates) runs as its child.
func (i OpenTelemetry) OnLanguageModelCallStart(ctx context.Context, e LanguageModelCallStartEvent) context.Context {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return ctx
	}
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := "chat"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	ctx, span := tracer.Start(ctx, spanName)
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeLanguageModel,
		OperationType: "ai.generateText",
		CallID:        e.CallID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	}
	if e.Temperature != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.temperature", *e.Temperature)
	}
	if e.MaxOutputTokens != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.max_tokens", *e.MaxOutputTokens))
	}
	if e.TopP != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.top_p", *e.TopP)
	}
	if e.TopK != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.top_k", *e.TopK))
	}
	if e.PresencePenalty != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.presence_penalty", *e.PresencePenalty)
	}
	if e.FrequencyPenalty != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.frequency_penalty", *e.FrequencyPenalty)
	}
	if len(e.StopSequences) > 0 {
		attrs = append(attrs, attribute.StringSlice("gen_ai.request.stop_sequences", e.StopSequences))
	}
	if e.Seed != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.seed", *e.Seed))
	}
	if (e.Settings == nil || e.Settings.RecordInputs) && e.System != "" {
		attrs = append(attrs, attribute.String("gen_ai.system_instructions", e.System))
	}
	if (e.Settings == nil || e.Settings.RecordInputs) && e.Prompt != nil {
		if b, err := json.Marshal(e.Prompt); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.input.messages", string(b)))
		}
	}
	if toolDefs, ok := toolDefinitionsJSON(e.Tools); ok {
		attrs = append(attrs, attribute.String("gen_ai.tool.definitions", toolDefs))
	}
	span.SetAttributes(attrs...)
	if e.CallID != "" {
		genAICallSpans.Store(genAISpanKey("languageModel", e.CallID), otelSpanEntry{span: span})
	}
	return ctx
}

// toolDefinitionsJSON encodes a []types.Tool (or nil) as a compact JSON array
// of {name, description}, matching TS's gen_ai.tool.definitions shape.
func toolDefinitionsJSON(tools interface{}) (string, bool) {
	list, ok := tools.([]types.Tool)
	if !ok || len(list) == 0 {
		return "", false
	}
	type toolDef struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	defs := make([]toolDef, 0, len(list))
	for _, t := range list {
		defs = append(defs, toolDef{Name: t.Name, Description: t.Description})
	}
	b, err := json.Marshal(defs)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// OnLanguageModelCallEnd records response attributes and ends the "chat" span.
func (i OpenTelemetry) OnLanguageModelCallEnd(_ context.Context, e LanguageModelCallEndEvent) {
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("languageModel", e.CallID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}),
	}
	if e.ResponseID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.id", e.ResponseID))
	}
	if e.ModelID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.model", e.ModelID))
	}
	attrs = appendGenAIUsageAttrs(attrs, e.Usage, i.opts.Usage)
	attrs = setFiniteFloat64(attrs, "gen_ai.client.operation.duration", msToSeconds(e.Performance.ResponseTimeMs))
	if e.Performance.TimeToFirstOutputMs != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.client.operation.time_to_first_chunk", msToSeconds(*e.Performance.TimeToFirstOutputMs))
	}
	if stats := e.Performance.TimeBetweenOutputChunksMs; stats != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.client.operation.time_per_output_chunk", msToSeconds(stats.Median)/1000)
	}
	if (e.Settings == nil || e.Settings.RecordOutputs) && e.Content != nil {
		if b, err := json.Marshal(e.Content); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.output.messages", string(b)))
		}
	}
	entry.span.SetAttributes(attrs...)
	entry.span.End()
}

func appendGenAIUsageAttrs(attrs []attribute.KeyValue, usage TelemetryUsage, legacyUsage bool) []attribute.KeyValue {
	if usage.InputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.input_tokens", *usage.InputTokens))
	}
	if usage.OutputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.output_tokens", *usage.OutputTokens))
	}
	if usage.CacheReadInputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.cache_read.input_tokens", *usage.CacheReadInputTokens))
	}
	if usage.CacheCreationInputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.cache_creation.input_tokens", *usage.CacheCreationInputTokens))
	}
	if legacyUsage {
		if usage.TotalTokens != nil {
			attrs = append(attrs, attribute.Int64("ai.usage.totalTokens", *usage.TotalTokens))
		}
		if usage.ReasoningTokens != nil {
			attrs = append(attrs, attribute.Int64("ai.usage.reasoningTokens", *usage.ReasoningTokens))
		}
	}
	return attrs
}

// OnEmbedStart creates the embeddings request span.
func (i OpenTelemetry) OnEmbedStart(ctx context.Context, e EmbeddingModelCallStartEvent) {
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
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeEmbedding,
		OperationType: e.OperationID,
		CallID:        callID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", mapOperationName(e.OperationID)),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if callID != "" {
		genAICallSpans.Store(genAISpanKey("embedding", callID), otelSpanEntry{span: span})
	}
}

// OnEmbedEnd records usage on the embeddings request span only — NOT on the
// root ai.embed/ai.embedMany span, avoiding the double count TS fixed in
// c0a42bc (the root span's OnEnd omits gen_ai.usage.input_tokens for embed
// operations; see OnEnd below).
func (i OpenTelemetry) OnEmbedEnd(_ context.Context, e EmbeddingModelCallEndEvent) {
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("embedding", callID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.Usage.InputTokens > 0 {
		entry.span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", e.Usage.InputTokens))
	}
	entry.span.End()
}

// OnRerankStart creates the reranking request span.
func (i OpenTelemetry) OnRerankStart(ctx context.Context, e RerankingModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.CallID, e.OperationID)
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := "rerank"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(ctx, spanName)
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeReranking,
		OperationType: e.OperationID,
		CallID:        callID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "rerank"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if callID != "" {
		genAICallSpans.Store(genAISpanKey("reranking", callID), otelSpanEntry{span: span})
	}
}

// OnRerankEnd ends the reranking request span.
func (i OpenTelemetry) OnRerankEnd(_ context.Context, e RerankingModelCallEndEvent) {
	callID := modelCallID(e.CallID, e.OperationID)
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("reranking", callID))
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

// OnToolExecutionStart creates an "execute_tool {name}" span for a locally
// executed tool call, matching TS's gen_ai.operation.name=execute_tool
// shape (37b75e8).
func (i OpenTelemetry) OnToolExecutionStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx
	}
	tracer := span.TracerProvider().Tracer("go-ai")
	ctx, child := tracer.Start(ctx, "execute_tool "+e.ToolName)
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType: SpanTypeTool,
		CallID:   e.ToolCallID,
	}); len(attrs) > 0 {
		child.SetAttributes(attrs...)
	}
	child.SetAttributes(
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.call.id", e.ToolCallID),
		attribute.String("gen_ai.tool.name", e.ToolName),
		attribute.String("gen_ai.tool.type", "function"),
	)
	if i.opts.RuntimeContext {
		if attrs, ok := ctx.Value(genAIRuntimeContextKey{}).([]attribute.KeyValue); ok {
			child.SetAttributes(attrs...)
		}
	}
	return ctx
}

// OnToolExecutionEnd ends the execute_tool span.
func (i OpenTelemetry) OnToolExecutionEnd(ctx context.Context, e TelemetryToolCallFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(setFiniteFloat64(nil, "gen_ai.execute_tool.duration", msToSeconds(e.DurationMs))...)
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End()
}

// OnToolCallStart/OnToolCallFinish are the deprecated Go names, kept for
// TelemetryIntegration interface satisfaction.
func (i OpenTelemetry) OnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	return i.OnToolExecutionStart(ctx, e)
}
func (i OpenTelemetry) OnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent) {
	i.OnToolExecutionEnd(ctx, e)
}

// OnStepEnd records step-level attributes, creates short-lived execute_tool
// spans for any provider-executed tool calls surfaced this step (5ad6abf),
// and ends the step span created by OnStepStart.
func (i OpenTelemetry) OnStepEnd(ctx context.Context, e TelemetryStepEndEvent) {
	stepSpan, ok := ctx.Value(genAIStepSpanKey{}).(trace.Span)
	if !ok || !stepSpan.IsRecording() {
		return
	}
	recordOutputs := e.Settings != nil && e.Settings.RecordOutputs
	attrs := []attribute.KeyValue{
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}),
	}
	if e.ResponseID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.id", e.ResponseID))
	}
	if e.ResponseModelID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.model", e.ResponseModelID))
	}
	attrs = appendGenAIUsageAttrs(attrs, e.Usage, i.opts.Usage)
	if i.opts.ProviderMetadata && recordOutputs && e.ProviderMetadata != nil {
		if b, err := json.Marshal(e.ProviderMetadata); err == nil {
			attrs = append(attrs, attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
	stepSpan.SetAttributes(attrs...)

	// Provider-executed tool calls have no separate OnToolExecutionStart/End
	// pair (the provider ran them), so synthesize a short execute_tool span
	// for each one here, using the step span as parent.
	for _, tc := range e.ToolCalls {
		if !tc.ProviderExecuted {
			continue
		}
		tracer := stepSpan.TracerProvider().Tracer("go-ai")
		_, toolSpan := tracer.Start(ctx, "execute_tool "+tc.ToolName)
		toolSpan.SetAttributes(
			attribute.String("gen_ai.operation.name", "execute_tool"),
			attribute.String("gen_ai.tool.call.id", tc.ID),
			attribute.String("gen_ai.tool.name", tc.ToolName),
			attribute.String("gen_ai.tool.type", "extension"),
		)
		toolSpan.End()
	}

	stepSpan.End()
}

// OnFinish is the deprecated compatibility alias for OnEnd.
func (i OpenTelemetry) OnFinish(ctx context.Context, e TelemetryFinishEvent) { i.OnEnd(ctx, e) }

// OnEnd sets final output attributes on the root span and ends it. It omits
// gen_ai.usage.input_tokens for embed/embedMany operations to avoid the
// double count fixed in c0a42bc — that count already lives on the
// embeddings request span from OnEmbedEnd.
func (i OpenTelemetry) OnEnd(ctx context.Context, e TelemetryFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	isEmbedOperation := spanNameLooksLikeEmbed(e)
	attrs := []attribute.KeyValue{
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}),
	}
	if e.ModelProvider != "" {
		attrs = append(attrs, attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)))
	}
	if e.ModelID != "" {
		attrs = append(attrs, attribute.String("gen_ai.request.model", e.ModelID))
	}
	if !isEmbedOperation {
		attrs = appendGenAIUsageAttrs(attrs, e.Usage, i.opts.Usage)
	} else if e.Usage.OutputTokens != nil {
		// Non-embed-input-token usage (rare for embeddings) is still safe to
		// report; only input_tokens is the documented double count.
		attrs = append(attrs, attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}
	span.SetAttributes(attrs...)
	span.End()
}

// spanNameLooksLikeEmbed reports whether this TelemetryFinishEvent likely
// belongs to an embed/embedMany operation. TelemetryFinishEvent carries no
// OperationType field, so this is inferred from the presence of a
// TotalTokens-only usage shape (Embed/EmbedMany report only TotalTokens, see
// telemetryUsageFromEmbeddingUsage in pkg/ai/embed.go), matching the
// embedding usage shape distinctly from chat usage (which always has
// InputTokens/OutputTokens split).
func spanNameLooksLikeEmbed(e TelemetryFinishEvent) bool {
	return e.Usage.TotalTokens != nil && e.Usage.InputTokens == nil && e.Usage.OutputTokens == nil
}

// OnError records the error on the root span (with HTTP status when
// available) and ends it.
func (i OpenTelemetry) OnError(ctx context.Context, e TelemetryErrorEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(span, e.Error)
	}
	span.End()
}

// OnAbort ends any in-flight model-call span and the root span.
func (i OpenTelemetry) OnAbort(ctx context.Context, e TelemetryAbortEvent) {
	if e.CallID != "" {
		if value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("languageModel", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				entry.span.End()
			}
		}
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.End()
}

// ExecuteTool delegates directly to execute; nested span support is handled
// by OnToolExecutionStart/End instead.
func (i OpenTelemetry) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func noopTracer() trace.Tracer {
	return noop.NewTracerProvider().Tracer(TracerName)
}

var _ TelemetryIntegration = OpenTelemetry{}
var _ TelemetryIntegration = LegacyOpenTelemetry{}

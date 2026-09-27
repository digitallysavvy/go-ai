package telemetry

import (
	"context"
	"encoding/json"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ---------------------------------------------------------------------------
// OpenTelemetry: GenAI semantic-convention integration
// ---------------------------------------------------------------------------
//
// mapProviderName, mapOperationName, and the gen_ai.input.messages /
// gen_ai.output.messages / gen_ai.system_instructions formatters live in
// gen_ai_format_messages.go (the Go port of TS's
// otel/src/gen-ai-format-messages.ts).

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
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if recordInputs && e.System != "" {
		if b, err := json.Marshal(formatSystemInstructions(e.System)); err == nil {
			span.SetAttributes(attribute.String("gen_ai.system_instructions", string(b)))
		}
	}
	if recordInputs && len(e.Messages) > 0 {
		if b, err := json.Marshal(formatInputMessages(e.Messages)); err == nil {
			span.SetAttributes(attribute.String("gen_ai.input.messages", string(b)))
		}
	}
	if i.opts.Embedding && e.OperationType == "ai.embedMany" && e.ValueCount > 0 {
		span.SetAttributes(attribute.Int("ai.values.count", e.ValueCount))
	}
	if i.opts.Headers {
		for _, attr := range headerAttributes(e.Headers) {
			span.SetAttributes(attr)
		}
	}
	if i.opts.Schema {
		if len(e.Schema) > 0 {
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
	}
	if i.opts.RuntimeContext {
		if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
			span.SetAttributes(attrs...)
			ctx = context.WithValue(ctx, genAIRuntimeContextKey{}, attrs)
		}
	}
	return ctx
}

// headerAttributes converts caller-supplied request headers into
// ai.request.headers.* attributes, mirroring TS's getHeaderAttributes
// (otel/src/supplemental-attributes.ts).
func headerAttributes(headers map[string]string) []attribute.KeyValue {
	if len(headers) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(headers))
	for k, v := range headers {
		attrs = append(attrs, attribute.String("ai.request.headers."+k, v))
	}
	return attrs
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
	if i.opts.ToolChoice && e.ToolChoice.Type != "" {
		if b, err := json.Marshal(e.ToolChoice); err == nil {
			stepSpan.SetAttributes(attribute.String("ai.prompt.toolChoice", string(b)))
		}
	}
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
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if recordInputs && e.System != "" {
		if b, err := json.Marshal(formatSystemInstructions(e.System)); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.system_instructions", string(b)))
		}
	}
	if recordInputs {
		if messages, ok := promptMessages(e.Prompt); ok && len(messages) > 0 {
			if b, err := json.Marshal(formatInputMessages(messages)); err == nil {
				attrs = append(attrs, attribute.String("gen_ai.input.messages", string(b)))
			}
		}
	}
	toolDefs := toolDefinitionsList(e.Tools)
	if len(toolDefs) > 0 {
		if b, err := json.Marshal(toolDefs); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.tool.definitions", string(b)))
		}
	}
	span.SetAttributes(attrs...)
	if e.CallID != "" {
		genAICallSpans.Store(genAISpanKey("languageModel", e.CallID), otelSpanEntry{span: span, toolDefs: toolDefs})
	}
	return ctx
}

// toolDefinitionsList builds the gen_ai.tool.definitions SemConv value (a
// list of {name, description?} objects) from the tools declared for this
// call, matching TS's gen_ai.tool.definitions shape. The returned slice is
// also retained on the span entry so OnLanguageModelCallEnd can append
// provider-executed tools observed only in the response (5ad6abf).
func toolDefinitionsList(tools interface{}) []map[string]interface{} {
	list, ok := tools.([]types.Tool)
	if !ok || len(list) == 0 {
		return nil
	}
	defs := make([]map[string]interface{}, 0, len(list))
	for _, t := range list {
		def := map[string]interface{}{"name": t.Name}
		if t.Description != "" {
			def["description"] = t.Description
		}
		defs = append(defs, def)
	}
	return defs
}

// mergeProviderExecutedToolDefs appends {type:"extension", name} entries for
// any provider-executed tool call in content whose name isn't already in
// existing, mirroring TS's onLanguageModelCallEnd toolDefinitions merge
// (5ad6abf). Returns the (possibly extended) list and whether it changed.
func mergeProviderExecutedToolDefs(existing []map[string]interface{}, content []types.ContentPart) ([]map[string]interface{}, bool) {
	names := make(map[string]bool, len(existing))
	for _, d := range existing {
		if n, ok := d["name"].(string); ok {
			names[n] = true
		}
	}
	merged := existing
	changed := false
	for _, c := range content {
		tc, ok := c.(types.ToolCallContent)
		if !ok || !tc.ProviderExecuted || names[tc.ToolName] {
			continue
		}
		merged = append(merged, map[string]interface{}{"type": "extension", "name": tc.ToolName})
		names[tc.ToolName] = true
		changed = true
	}
	return merged, changed
}

// contentParts extracts a []types.ContentPart from a LanguageModelCallEndEvent's
// untyped Content field, when that's the concrete type it holds.
func contentParts(content interface{}) []types.ContentPart {
	parts, _ := content.([]types.ContentPart)
	return parts
}

// finalToolResultsByCallID indexes the final (non-preliminary) tool result or
// error for each tool call id in content, mirroring TS's finalToolOutputs map.
func finalToolResultsByCallID(content []types.ContentPart) map[string]types.ContentPart {
	out := make(map[string]types.ContentPart)
	for _, c := range content {
		switch p := c.(type) {
		case types.ToolResultContent:
			if !p.Preliminary {
				out[p.ToolCallID] = p
			}
		case types.ToolErrorContent:
			out[p.ToolCallID] = p
		}
	}
	return out
}

// OnLanguageModelCallEnd records response attributes, ends the "chat" span,
// and creates short-lived execute_tool spans for provider-executed tool
// calls surfaced in the response (37b75e8), parented under the chat span.
func (i OpenTelemetry) OnLanguageModelCallEnd(ctx context.Context, e LanguageModelCallEndEvent) {
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("languageModel", e.CallID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	content := contentParts(e.Content)
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
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	if recordOutputs && len(content) > 0 {
		if b, err := json.Marshal(formatOutputMessagesFromContent(content, e.FinishReason)); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.output.messages", string(b)))
		}
	}
	if i.opts.ProviderMetadata && recordOutputs && e.ProviderMetadata != nil {
		if b, err := json.Marshal(e.ProviderMetadata); err == nil {
			attrs = append(attrs, attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
	mergedToolDefs, changed := mergeProviderExecutedToolDefs(entry.toolDefs, content)
	if changed {
		if b, err := json.Marshal(mergedToolDefs); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.tool.definitions", string(b)))
		}
	}
	entry.span.SetAttributes(attrs...)

	inferenceCtx := trace.ContextWithSpan(ctx, entry.span)
	tracer := entry.span.TracerProvider().Tracer("go-ai")
	finalOutputs := finalToolResultsByCallID(content)
	recorded := make(map[string]bool)
	for _, c := range content {
		tc, ok := c.(types.ToolCallContent)
		if !ok || !tc.ProviderExecuted || recorded[tc.ToolCallID] {
			continue
		}
		recorded[tc.ToolCallID] = true
		_, toolSpan := tracer.Start(inferenceCtx, "execute_tool "+tc.ToolName)
		toolSpan.SetAttributes(
			attribute.String("gen_ai.operation.name", "execute_tool"),
			attribute.String("gen_ai.tool.name", tc.ToolName),
			attribute.String("gen_ai.tool.call.id", tc.ToolCallID),
			attribute.String("gen_ai.tool.type", "extension"),
		)
		switch out := finalOutputs[tc.ToolCallID].(type) {
		case types.ToolResultContent:
			if b, err := json.Marshal(toolResultResponseValue(out)); err == nil {
				toolSpan.SetAttributes(attribute.String("gen_ai.tool.call.result", string(b)))
			}
		case types.ToolErrorContent:
			if err, ok := out.Error.(error); ok {
				RecordErrorOnSpan(toolSpan, err)
			} else if out.Error != nil {
				toolSpan.SetStatus(codes.Error, jsonOrString(out.Error))
			}
		}
		toolSpan.End()
	}

	entry.span.End()
}

func jsonOrString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return ""
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
	if i.opts.Embedding && (e.Settings == nil || e.Settings.RecordInputs) && len(e.Values) > 0 {
		span.SetAttributes(attribute.StringSlice("ai.values", jsonStringifyEach(e.Values)))
	}
	if callID != "" {
		genAICallSpans.Store(genAISpanKey("embedding", callID), otelSpanEntry{span: span})
	}
}

// jsonStringifyEach JSON-encodes each value individually, matching TS's
// `values.map(v => JSON.stringify(v))` pattern used for ai.values/ai.documents.
func jsonStringifyEach[T any](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		if b, err := json.Marshal(v); err == nil {
			out[i] = string(b)
		}
	}
	return out
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
	if i.opts.Embedding && (e.Settings == nil || e.Settings.RecordOutputs) && len(e.Embeddings) > 0 {
		entry.span.SetAttributes(attribute.StringSlice("ai.embeddings", jsonStringifyEach(e.Embeddings)))
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
	if i.opts.Reranking && (e.Settings == nil || e.Settings.RecordInputs) {
		if docs, ok := e.Documents.([]string); ok && len(docs) > 0 {
			span.SetAttributes(attribute.StringSlice("ai.documents", jsonStringifyEach(docs)))
		}
	}
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
	if i.opts.Reranking && (e.Settings == nil || e.Settings.RecordOutputs) && len(e.Ranking) > 0 {
		entry.span.SetAttributes(
			attribute.String("ai.ranking.type", e.DocumentsType),
			attribute.StringSlice("ai.ranking", jsonStringifyEach(e.Ranking)),
		)
	}
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

	// Provider-executed tool calls get their execute_tool span from
	// OnLanguageModelCallEnd (parented under the chat span), matching TS's
	// onLanguageModelCallEnd (37b75e8) — TS's onStepEnd does not create tool
	// spans. A synthesis here as well would double-emit one execute_tool
	// span per provider-executed call in normal use, since every step always
	// goes through OnLanguageModelCallEnd first.
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

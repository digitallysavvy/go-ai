package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
	// ExperimentalDecision emits ai.decision.* attributes.
	ExperimentalDecision bool
	// ExperimentalEvaluation is the pre-rename form of ExperimentalDecision;
	// either one enables ai.decision.* attributes.
	//
	// Deprecated: Use ExperimentalDecision instead.
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

// experimentalDecision reports whether ai.decision.* attributes are enabled,
// honoring the deprecated ExperimentalEvaluation option. Mirrors TS's
// `experimental_decision ?? experimental_evaluation ?? false`.
func (o OpenTelemetryOptions) experimentalDecision() bool {
	return o.ExperimentalDecision || o.ExperimentalEvaluation
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

// genAICallState is OpenTelemetry's (the GenAI semconv integration's) own
// per-call root/step/tool span bookkeeping, keyed by CallID (H5). It mirrors
// legacyCallState in registry.go — same rationale, separate map, so the two
// integrations' state can never collide: FireOnStart/FireOnStepStart/etc
// thread ctx through both integrations in turn, and OTel's
// trace.SpanFromContext(ctx) only ever resolves the single "current" span in
// ctx, so relying on it here would silently resolve to LegacyOpenTelemetry's
// span (or vice versa) whichever integration ran last, leaking this
// integration's own root/step spans forever. Every method below resolves its
// OWN parent/span from this state and explicitly rebases ctx onto it via
// trace.ContextWithSpan before starting a child span, so nesting stays
// correct regardless of what the other integration did to ctx.
type genAICallState struct {
	mu       sync.Mutex
	rootSpan trace.Span
	// baseSupp holds the root span's supplemental runtime-context/header
	// attributes (TS baseSupplementalAttributes) for the step, embeddings
	// and reranking child spans.
	baseSupp []attribute.KeyValue
	// tracer is the tracer that created the root span (TS uses this.tracer
	// for every span of the integration).
	tracer    trace.Tracer
	stepSpan  trace.Span
	toolSpans map[string]trace.Span
	// embedSpans/rerankSpan mirror LegacyOpenTelemetry's own fields in
	// registry.go — see the doc comment there. Closed defensively by
	// OnError/OnAbort so a provider failure that skips
	// OnEmbedEnd/OnRerankEnd doesn't leak them.
	embedSpans map[string]trace.Span
	rerankSpan trace.Span
}

var genAICallStates sync.Map // map[string]*genAICallState, keyed by CallID

// genAIState returns (creating if necessary) OpenTelemetry's call state for
// callID. Returns nil for an empty callID.
func genAIState(callID string) *genAICallState {
	if callID == "" {
		return nil
	}
	v, _ := genAICallStates.LoadOrStore(callID, &genAICallState{})
	return v.(*genAICallState)
}

// genAIBaseSupplemental returns the root span's supplemental attributes
// (runtime context and headers) recorded for callID.
func genAIBaseSupplemental(callID string) []attribute.KeyValue {
	st := genAIState(callID)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.baseSupp
}

// genAIDeleteState removes callID's call state once its root span has ended.
func genAIDeleteState(callID string) {
	if callID != "" {
		genAICallStates.Delete(callID)
	}
}

// genAIRootSpanFor resolves OpenTelemetry's own root span for callID,
// falling back to trace.SpanFromContext(ctx) when callID is empty or has no
// recorded root span yet (defensive: an event from a call site that hasn't
// been migrated to populate CallID). Single-integration behavior is
// unaffected either way.
func genAIRootSpanFor(callID string, ctx context.Context) trace.Span {
	if st := genAIState(callID); st != nil {
		st.mu.Lock()
		span := st.rootSpan
		st.mu.Unlock()
		if span != nil {
			return span
		}
	}
	return trace.SpanFromContext(ctx)
}

// genAIChildTracer returns the tracer that created callID's root span, so
// child spans use the same (possibly caller-supplied) tracer as TS's
// this.tracer; it falls back to the parent span's provider.
func genAIChildTracer(callID string, parent trace.Span) trace.Tracer {
	if st := genAIState(callID); st != nil {
		st.mu.Lock()
		t := st.tracer
		st.mu.Unlock()
		if t != nil {
			return t
		}
	}
	return parent.TracerProvider().Tracer("go-ai")
}

func isAudioOperation(operationType string) bool {
	return operationType == "ai.generateSpeech" || operationType == "ai.transcribe" || operationType == "ai.streamTranscribe"
}

// genAIRootSpanKind mirrors the SpanKind TS gives each root span: INTERNAL for
// generate/stream text and object operations, CLIENT for audio, embed, rerank
// and decide operations.
func genAIRootSpanKind(operationType string) trace.SpanKind {
	switch operationType {
	case "ai.generateObject", "ai.streamObject":
		return trace.SpanKindInternal
	case "ai.generateSpeech", "ai.transcribe", "ai.streamTranscribe",
		"ai.embed", "ai.embedMany", "ai.rerank":
		return trace.SpanKindClient
	}
	return trace.SpanKindInternal
}

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
	// Root span name is "${operationName} ${modelId}" (TS's onGenerateStart/
	// onObjectOperationStart/onEmbedOperationStart/onRerankOperationStart,
	// open-telemetry.ts): the GenAI-mapped operation name (mapOperationName),
	// never the raw operationId, and the model id — not functionId, which
	// only ever surfaces via the gen_ai.agent.name attribute below (H3
	// follow-up 5).
	spanName := mapOperationName(e.OperationType)
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(genAIRootSpanKind(e.OperationType)))
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
	// TS sets gen_ai.agent.name on the generate, object and audio roots only;
	// the embed and rerank roots carry no function id.
	if e.Settings != nil && e.Settings.FunctionID != "" &&
		(genAIRootSpanKind(e.OperationType) == trace.SpanKindInternal || isAudioOperation(e.OperationType)) {
		span.SetAttributes(attribute.String("gen_ai.agent.name", e.Settings.FunctionID))
	}
	// TS onGenerateStart / onObjectOperationStart also put the call-level
	// request settings on the root span; embed/rerank roots carry none.
	// stop_sequences is a generateText/streamText setting only, and the
	// generateObject/streamObject root declares its output type.
	isObjectOperation := e.OperationType == "ai.generateObject" || e.OperationType == "ai.streamObject"
	if isObjectOperation {
		span.SetAttributes(attribute.String("gen_ai.output.type", "json"))
	}
	span.SetAttributes(requestSettingAttributes(e, !isObjectOperation)...)
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if recordInputs && e.System != "" {
		if b, err := jsonstr.Marshal(formatSystemInstructions(e.System)); err == nil {
			span.SetAttributes(attribute.String("gen_ai.system_instructions", string(b)))
		}
	}
	if recordInputs && len(e.Messages) > 0 {
		if b, err := jsonstr.Marshal(formatInputMessages(e.Messages)); err == nil {
			span.SetAttributes(attribute.String("gen_ai.input.messages", string(b)))
		}
	}
	// onAudioOperationStart (TS open-telemetry.ts): "ai.generateSpeech",
	// "ai.transcribe", and "ai.streamTranscribe" additionally carry
	// gen_ai.output.type/request.stream and the text/audio request content,
	// none of which apply to any other operation type.
	if e.OperationType == "ai.generateSpeech" || e.OperationType == "ai.transcribe" || e.OperationType == "ai.streamTranscribe" {
		outputType := "text"
		if e.OperationType == "ai.generateSpeech" {
			outputType = "speech"
		}
		span.SetAttributes(
			attribute.String("gen_ai.output.type", outputType),
			attribute.Bool("gen_ai.request.stream", e.OperationType == "ai.streamTranscribe"),
		)
		if recordInputs && e.Text != "" {
			span.SetAttributes(attribute.String("ai.request.text", e.Text))
			if b, err := jsonstr.Marshal([]semConvInputMessage{{
				Role:  "user",
				Parts: []semConvPart{{"type": "text", "content": e.Text}},
			}}); err == nil {
				span.SetAttributes(attribute.String("gen_ai.input.messages", string(b)))
			}
		}
		if recordInputs && e.AudioByteLength != nil {
			span.SetAttributes(attribute.Int64("ai.request.audio.size", *e.AudioByteLength))
		}
		if recordInputs && e.AudioMediaType != "" {
			span.SetAttributes(attribute.String("ai.request.audio.media_type", e.AudioMediaType))
		}
	}
	if i.opts.Headers {
		for _, attr := range headerAttributes(e.Headers) {
			span.SetAttributes(attr)
		}
	}
	if i.opts.Schema {
		if len(e.Schema) > 0 {
			if b, err := jsonstr.Marshal(e.Schema); err == nil {
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
	// TS baseSupplementalAttributes: the runtime-context and header
	// attributes of the root span, reused by the step, embeddings and
	// reranking child spans (not by chat or tool spans).
	var baseSupp []attribute.KeyValue
	if i.opts.RuntimeContext {
		if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
			span.SetAttributes(attrs...)
			ctx = context.WithValue(ctx, genAIRuntimeContextKey{}, attrs)
			baseSupp = append(baseSupp, attrs...)
		}
	}
	if i.opts.Headers {
		baseSupp = append(baseSupp, headerAttributes(e.Headers)...)
	}
	// H5: record this integration's own root span in callId-keyed state, so
	// OnStepStart/OnToolExecutionStart/OnEnd/OnError/OnAbort can find it by
	// CallID instead of trace.SpanFromContext.
	if e.CallID != "" {
		st := genAIState(e.CallID)
		st.mu.Lock()
		st.rootSpan = span
		st.baseSupp = baseSupp
		st.tracer = tracer
		st.mu.Unlock()
	}
	return ctx
}

// requestSettingAttributes returns the gen_ai.request.* attributes for the
// call-level generation settings of a start event (TS onGenerateStart /
// onObjectOperationStart). Unset settings are omitted.
func requestSettingAttributes(e TelemetryStartEvent, includeStopSequences bool) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if e.FrequencyPenalty != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.frequency_penalty", *e.FrequencyPenalty)
	}
	if e.MaxOutputTokens != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.max_tokens", *e.MaxOutputTokens))
	}
	if e.PresencePenalty != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.presence_penalty", *e.PresencePenalty)
	}
	if e.Temperature != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.temperature", *e.Temperature)
	}
	if e.TopK != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.top_k", *e.TopK))
	}
	if e.TopP != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.request.top_p", *e.TopP)
	}
	if includeStopSequences && len(e.StopSequences) > 0 {
		attrs = append(attrs, attribute.StringSlice("gen_ai.request.stop_sequences", e.StopSequences))
	}
	if e.Seed != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.seed", *e.Seed))
	}
	return attrs
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
	// H5: resolve OUR OWN root span by CallID rather than
	// trace.SpanFromContext(ctx), which — with two integrations registered —
	// would return whichever integration's OnStart ran last.
	rootSpan := genAIRootSpanFor(e.CallID, ctx)
	if !rootSpan.IsRecording() {
		return ctx
	}
	tracer := genAIChildTracer(e.CallID, rootSpan)
	// Explicitly rebase ctx onto our own root span before creating the step
	// span, so it nests correctly under this integration's root even if
	// ctx's ambient "current span" belongs to another registered integration.
	ctx = trace.ContextWithSpan(ctx, rootSpan)
	// TS names the span "step ${steps.length + 1}" — steps.length is the
	// count of steps completed BEFORE this one, so the first step is "step
	// 1". Go's StepNumber is 0-indexed (stepIndex), so add 1 (H3 follow-up 5).
	spanName := "step " + itoa(e.StepNumber+1)
	ctx, stepSpan := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeStep,
		OperationType:  e.OperationType,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		stepSpan.SetAttributes(attrs...)
	}
	stepSpan.SetAttributes(attribute.String("gen_ai.operation.name", "agent_step"))
	stepSpan.SetAttributes(genAIBaseSupplemental(e.CallID)...)
	if i.opts.ToolChoice && e.ToolChoice.Type != "" {
		if b, err := jsonstr.Marshal(e.ToolChoice); err == nil {
			stepSpan.SetAttributes(attribute.String("ai.prompt.toolChoice", string(b)))
		}
	}
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			st.stepSpan = stepSpan
			st.mu.Unlock()
		}
	}
	return context.WithValue(ctx, genAIStepSpanKey{}, stepSpan)
}

// OnLanguageModelCallStart creates the "chat" model-call span with GenAI
// request parameters and returns it embedded in ctx (594029e), so the
// provider call (and any HTTP spans it creates) runs as its child.
func (i OpenTelemetry) OnLanguageModelCallStart(ctx context.Context, e LanguageModelCallStartEvent) context.Context {
	// H5: parent the "chat" span under OUR OWN step span, resolved by CallID
	// rather than trace.SpanFromContext(ctx) (which — with two integrations
	// registered — could return the other integration's "current" span).
	// TS's onLanguageModelCallStart requires state.stepContext strictly, with
	// no root-span fallback (`if (!state?.stepContext) return;`,
	// packages/otel/src/open-telemetry.ts). Go used to need a root-span
	// fallback here because streamText issued each step's provider call
	// before that step's FireOnStepStart had run; the "ST" fix (stream.go's
	// bootstrapAndStream/processStream) now fires FireOnStepStart before
	// every step's DoStream call, so st.stepSpan is always set by the time
	// this runs and the fallback is dead — removed to match TS exactly (see
	// TestStreamTextGenAIChatSpanNestsUnderStepSpanForFirstStep).
	var parent trace.Span
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			parent = st.stepSpan
			st.mu.Unlock()
		}
	}
	if parent == nil || !parent.IsRecording() {
		return ctx
	}
	tracer := genAIChildTracer(e.CallID, parent)
	spanName := "chat"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	ctx, span := tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, trace.WithSpanKind(trace.SpanKindClient))
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
		if b, err := jsonstr.Marshal(formatSystemInstructions(e.System)); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.system_instructions", string(b)))
		}
	}
	if recordInputs {
		if messages, ok := promptMessages(e.Prompt); ok && len(messages) > 0 {
			if b, err := jsonstr.Marshal(formatInputMessages(messages)); err == nil {
				attrs = append(attrs, attribute.String("gen_ai.input.messages", string(b)))
			}
		}
	}
	toolDefs := toolDefinitionsList(e.Tools)
	if recordInputs && len(toolDefs) > 0 {
		if b, err := jsonstr.Marshal(toolDefs); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.tool.definitions", string(b)))
		}
	}
	span.SetAttributes(attrs...)
	if e.CallID != "" {
		genAICallSpans.Store(genAISpanKey("languageModel", e.CallID), otelSpanEntry{span: span, toolDefs: toolDefs})
	}
	return ctx
}

// toolDefinitionsList builds the gen_ai.tool.definitions value from the tools
// declared for this call. TS stringifies the prepared language-model tools
// (prepareTools), so function tools carry {type:"function", name,
// inputSchema, description?, inputExamples?, providerOptions?, strict?} and
// provider tools {type:"provider", name, id, args}. The returned slice is
// also retained on the span entry so OnLanguageModelCallEnd can append
// provider-executed tools observed only in the response (5ad6abf).
func toolDefinitionsList(tools interface{}) []map[string]interface{} {
	list, ok := tools.([]types.Tool)
	if !ok || len(list) == 0 {
		return nil
	}
	defs := make([]map[string]interface{}, 0, len(list))
	for _, t := range list {
		if t.Type == "provider" {
			def := map[string]interface{}{"type": "provider", "name": t.Name, "id": t.ProviderID}
			if t.ProviderArgs != nil {
				def["args"] = t.ProviderArgs
			} else {
				def["args"] = map[string]interface{}{}
			}
			defs = append(defs, def)
			continue
		}
		def := map[string]interface{}{"type": "function", "name": t.Name, "inputSchema": toolInputSchema(t.Parameters)}
		if t.Description != "" {
			def["description"] = t.Description
		}
		if len(t.InputExamples) > 0 {
			examples := make([]map[string]interface{}, 0, len(t.InputExamples))
			for _, ex := range t.InputExamples {
				examples = append(examples, map[string]interface{}{"input": ex.Input})
			}
			def["inputExamples"] = examples
		}
		if t.ProviderOptions != nil {
			def["providerOptions"] = t.ProviderOptions
		}
		if t.Strict != nil {
			def["strict"] = *t.Strict
		}
		defs = append(defs, def)
	}
	return defs
}

// toolInputSchema returns the JSON Schema of a function tool for the tool
// definitions attribute, falling back to the empty object schema TS uses for
// a tool without input.
func toolInputSchema(parameters interface{}) interface{} {
	if parameters == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return parameters
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
	if e.FinishReason == string(types.FinishReasonError) {
		entry.span.SetStatus(codes.Error, "")
	}
	content := contentParts(e.Content)
	attrs := []attribute.KeyValue{
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}),
	}
	// TS re-records the provider on the chat span at call end, since the
	// responding provider can differ from the requested one.
	if e.ModelProvider != "" {
		attrs = append(attrs, attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)))
	}
	if e.ResponseID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.id", e.ResponseID))
	}
	if e.ModelID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.model", e.ModelID))
	}
	attrs = append(attrs, baseUsageAttrs(e.Usage, true)...)
	if i.opts.Usage {
		attrs = append(attrs, detailedUsageAttrs(e.Usage)...)
	}
	attrs = setFiniteFloat64(attrs, "gen_ai.client.operation.duration", msToSeconds(e.Performance.ResponseTimeMs))
	if e.Performance.TimeToFirstOutputMs != nil {
		attrs = setFiniteFloat64(attrs, "gen_ai.client.operation.time_to_first_chunk", msToSeconds(*e.Performance.TimeToFirstOutputMs))
	}
	if stats := e.Performance.TimeBetweenOutputChunksMs; stats != nil {
		// TS: msToSeconds(performance.timeBetweenOutputChunksMs?.avg).
		attrs = setFiniteFloat64(attrs, "gen_ai.client.operation.time_per_output_chunk", stats.Avg/1000)
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	// TS records gen_ai.output.messages for every call, an empty response
	// included ([{role, parts: [], finish_reason}]); only an event that carries
	// no content at all (nil) skips it.
	if recordOutputs && e.Content != nil {
		if b, err := jsonstr.Marshal(formatOutputMessagesFromContent(content, e.FinishReason)); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.output.messages", string(b)))
		}
	}
	// ai.response.providerMetadata is gated only by opts.ProviderMetadata, not
	// recordOutputs — TS's selectSupplementalAttributes/selectAttributes only
	// applies the recordOutputs gate to values wrapped in {output: () => ...};
	// providerMetadata is a plain pre-computed value in both
	// onLanguageModelCallEnd and onStepEnd (legacy-open-telemetry.ts) /
	// their GenAI equivalents (open-telemetry.ts), so only isEnabled and the
	// providerMetadata supplemental flag gate it.
	if i.opts.ProviderMetadata && e.ProviderMetadata != nil {
		if b, err := jsonstr.Marshal(e.ProviderMetadata); err == nil {
			attrs = append(attrs, attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
	mergedToolDefs, changed := mergeProviderExecutedToolDefs(entry.toolDefs, content)
	if changed && (e.Settings == nil || e.Settings.RecordInputs) {
		if b, err := jsonstr.Marshal(mergedToolDefs); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.tool.definitions", string(b)))
		}
	}
	entry.span.SetAttributes(attrs...)

	inferenceCtx := trace.ContextWithSpan(ctx, entry.span)
	tracer := genAIChildTracer(e.CallID, entry.span)
	finalOutputs := finalToolResultsByCallID(content)
	recorded := make(map[string]bool)
	for _, c := range content {
		tc, ok := c.(types.ToolCallContent)
		if !ok || !tc.ProviderExecuted || recorded[tc.ToolCallID] {
			continue
		}
		recorded[tc.ToolCallID] = true
		_, toolSpan := tracer.Start(inferenceCtx, "execute_tool "+tc.ToolName, trace.WithSpanKind(trace.SpanKindInternal))
		toolSpan.SetAttributes(
			attribute.String("gen_ai.operation.name", "execute_tool"),
			attribute.String("gen_ai.tool.name", tc.ToolName),
			attribute.String("gen_ai.tool.call.id", tc.ToolCallID),
			attribute.String("gen_ai.tool.type", "extension"),
		)
		// TS: gen_ai.tool.call.arguments is an input (recordInputs gate).
		if recordInputs := e.Settings == nil || e.Settings.RecordInputs; recordInputs {
			if args, ok := toolCallArgumentsJSON(tc); ok {
				toolSpan.SetAttributes(attribute.String("gen_ai.tool.call.arguments", args))
			}
		}
		switch out := finalOutputs[tc.ToolCallID].(type) {
		case types.ToolResultContent:
			if !recordOutputs {
				break
			}
			if b, err := jsonstr.Marshal(toolResultResponseValue(out)); err == nil {
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
	if b, err := jsonstr.Marshal(v); err == nil {
		return string(b)
	}
	return ""
}

// baseUsageAttrs returns the GenAI SemConv usage attributes. cacheCreation
// controls gen_ai.usage.cache_creation.input_tokens: TS records it for
// language-model calls and generateText/streamText roots but not for the
// generateObject/streamObject root.
func baseUsageAttrs(usage TelemetryUsage, cacheCreation bool) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if usage.InputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.input_tokens", *usage.InputTokens))
	}
	if usage.OutputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.output_tokens", *usage.OutputTokens))
	}
	if usage.CacheReadInputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.cache_read.input_tokens", *usage.CacheReadInputTokens))
	}
	if cacheCreation && usage.CacheCreationInputTokens != nil {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.cache_creation.input_tokens", *usage.CacheCreationInputTokens))
	}
	return attrs
}

// detailedUsageAttrs mirrors TS getDetailedUsageAttributes (emitted when
// OpenTelemetryOptions.Usage is set): the usage details GenAI SemConv has no
// attribute for.
func detailedUsageAttrs(usage TelemetryUsage) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if usage.NoCacheInputTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.inputTokenDetails.noCacheTokens", *usage.NoCacheInputTokens))
	}
	if usage.OutputTextTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.outputTokenDetails.textTokens", *usage.OutputTextTokens))
	}
	if usage.ReasoningTokens != nil {
		attrs = append(attrs, attribute.Int64("ai.usage.outputTokenDetails.reasoningTokens", *usage.ReasoningTokens))
	}
	return attrs
}

// OnEmbedStart creates the embeddings request span.
func (i OpenTelemetry) OnEmbedStart(ctx context.Context, e EmbeddingModelCallStartEvent) {
	// H5: resolve our own root span by CallID rather than
	// trace.SpanFromContext(ctx).
	parent := genAIRootSpanFor(e.CallID, ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	tracer := genAIChildTracer(e.CallID, parent)
	spanName := "embeddings"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, trace.WithSpanKind(trace.SpanKindClient))
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
	span.SetAttributes(genAIBaseSupplemental(e.CallID)...)
	if i.opts.Embedding && (e.Settings == nil || e.Settings.RecordInputs) && len(e.Values) > 0 {
		span.SetAttributes(attribute.StringSlice("ai.values", jsonStringifyEach(e.Values)))
	}
	if callID != "" {
		genAICallSpans.Store(genAISpanKey("embedding", callID), otelSpanEntry{span: span})
	}
	// H5: also record it in our own callId-keyed state so OnError/OnAbort can
	// defensively close it if the provider call itself fails before
	// OnEmbedEnd ever fires — see the identical comment in registry.go's
	// LegacyOpenTelemetry.OnEmbedStart.
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			if st.embedSpans == nil {
				st.embedSpans = make(map[string]trace.Span)
			}
			st.embedSpans[e.EmbedCallID] = span
			st.mu.Unlock()
		}
	}
}

// jsonStringifyEach JSON-encodes each value individually, matching TS's
// `values.map(v => JSON.stringify(v))` pattern used for ai.values/ai.documents.
func jsonStringifyEach[T any](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		if b, err := jsonstr.Marshal(v); err == nil {
			out[i] = string(b)
		}
	}
	return out
}

// OnEmbedEnd records usage on the embeddings request span only — NOT on the
// root ai.embed/ai.embedMany span, avoiding the double count TS fixed in
// c0a42bc (the root span's OnEnd omits gen_ai.usage.input_tokens for embed
// operations; see OnEnd below). When e.Error is set (a failed retry attempt
// — embed_many_batching.go/embed.go fire this event on every attempt's
// outcome, not just the one that ultimately succeeds), the span is closed
// with an error status instead, so no span is ever left open for OnEnd to
// leak — see the doc comment on EmbeddingModelCallEndEvent.
func (i OpenTelemetry) OnEmbedEnd(_ context.Context, e EmbeddingModelCallEndEvent) {
	callID := modelCallID(e.EmbedCallID, e.CallID, e.OperationID)
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("embedding", callID))
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			delete(st.embedSpans, e.EmbedCallID)
			st.mu.Unlock()
		}
	}
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(entry.span, e.Error)
		entry.span.End()
		return
	}
	if tokens, ok := embeddingUsageTokens(e.Usage); ok {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", tokens))
	}
	if i.opts.Embedding && (e.Settings == nil || e.Settings.RecordOutputs) && len(e.Embeddings) > 0 {
		entry.span.SetAttributes(attribute.StringSlice("ai.embeddings", jsonStringifyEach(e.Embeddings)))
	}
	entry.span.End()
}

// embeddingUsageTokens is TS usage.tokens for an embedding call: the
// TS-shaped Tokens field when the provider reported one, else the Go
// InputTokens/TotalTokens counters. A NaN (provider reported nothing) is
// dropped; a reported 0 is indistinguishable from the zero value and is
// dropped too (open item).
func embeddingUsageTokens(u types.EmbeddingUsage) (int64, bool) {
	tokens := u.Tokens
	if math.IsNaN(tokens) || math.IsInf(tokens, 0) || tokens <= 0 {
		tokens = float64(u.InputTokens)
		if tokens <= 0 {
			tokens = float64(u.TotalTokens)
		}
	}
	if tokens <= 0 {
		return 0, false
	}
	return int64(tokens), true
}

// OnRerankStart creates the reranking request span.
func (i OpenTelemetry) OnRerankStart(ctx context.Context, e RerankingModelCallStartEvent) {
	// H5: resolve our own root span by CallID rather than
	// trace.SpanFromContext(ctx).
	parent := genAIRootSpanFor(e.CallID, ctx)
	if !parent.IsRecording() {
		return
	}
	callID := modelCallID(e.CallID, e.OperationID)
	tracer := genAIChildTracer(e.CallID, parent)
	spanName := "rerank"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, trace.WithSpanKind(trace.SpanKindClient))
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
	span.SetAttributes(genAIBaseSupplemental(e.CallID)...)
	if i.opts.Reranking && (e.Settings == nil || e.Settings.RecordInputs) {
		if docs, ok := e.Documents.([]string); ok && len(docs) > 0 {
			span.SetAttributes(attribute.StringSlice("ai.documents", jsonStringifyEach(docs)))
		}
	}
	if callID != "" {
		genAICallSpans.Store(genAISpanKey("reranking", callID), otelSpanEntry{span: span})
	}
	// H5: also record it in our own callId-keyed state so OnError/OnAbort can
	// defensively close it if the provider call itself fails before
	// OnRerankEnd ever fires.
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			st.rerankSpan = span
			st.mu.Unlock()
		}
	}
}

// OnRerankEnd ends the reranking request span. When e.Error is set (a
// failed retry attempt — rerank.go fires this event on every attempt's
// outcome, not just the one that ultimately succeeds), the span is closed
// with an error status instead, so it is never orphaned by the next
// attempt's OnRerankStart overwriting st.rerankSpan — see the doc comment
// on RerankingModelCallEndEvent.
func (i OpenTelemetry) OnRerankEnd(_ context.Context, e RerankingModelCallEndEvent) {
	callID := modelCallID(e.CallID, e.OperationID)
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("reranking", callID))
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			st.rerankSpan = nil
			st.mu.Unlock()
		}
	}
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(entry.span, e.Error)
		entry.span.End()
		return
	}
	// TS onRerankEnd: ai.ranking.type is a plain value, ai.ranking an output;
	// there is no results-count attribute.
	if i.opts.Reranking {
		if e.DocumentsType != "" {
			entry.span.SetAttributes(attribute.String("ai.ranking.type", e.DocumentsType))
		}
		if (e.Settings == nil || e.Settings.RecordOutputs) && len(e.Ranking) > 0 {
			entry.span.SetAttributes(attribute.StringSlice("ai.ranking", jsonStringifyEach(e.Ranking)))
		}
	}
	entry.span.End()
}

// OnToolExecutionStart creates an "execute_tool {name}" span for a locally
// executed tool call, matching TS's gen_ai.operation.name=execute_tool
// shape (37b75e8).
func (i OpenTelemetry) OnToolExecutionStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	// H5: parent under OUR OWN step span, falling back to our own root span
	// — mirrors TS's onToolExecutionStart (open-telemetry.ts):
	// `state?.stepContext ?? state?.rootContext` — resolved by CallID rather
	// than trace.SpanFromContext(ctx) (which — with two integrations
	// registered — could return the other integration's "current" span).
	var span trace.Span
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			span = st.stepSpan
			if span == nil {
				span = st.rootSpan
			}
			st.mu.Unlock()
		}
	}
	if span == nil {
		span = trace.SpanFromContext(ctx)
	}
	if !span.IsRecording() {
		return ctx
	}
	tracer := genAIChildTracer(e.CallID, span)
	ctx, child := tracer.Start(trace.ContextWithSpan(ctx, span), "execute_tool "+e.ToolName, trace.WithSpanKind(trace.SpanKindInternal))
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
	// TS: gen_ai.tool.call.arguments is an input (recordInputs gate).
	if e.Settings == nil || e.Settings.RecordInputs {
		if b, err := jsonstr.Marshal(e.Args); err == nil {
			child.SetAttributes(attribute.String("gen_ai.tool.call.arguments", string(b)))
		}
	}
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			if st.toolSpans == nil {
				st.toolSpans = make(map[string]trace.Span)
			}
			st.toolSpans[e.ToolCallID] = child
			st.mu.Unlock()
		}
	}
	return ctx
}

// OnToolExecutionEnd ends the execute_tool span.
func (i OpenTelemetry) OnToolExecutionEnd(ctx context.Context, e TelemetryToolCallFinishEvent) {
	// H5: look up OUR OWN tool span by (CallID, ToolCallID) instead of
	// trace.SpanFromContext(ctx).
	var span trace.Span
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			span = st.toolSpans[e.ToolCallID]
			if span != nil {
				delete(st.toolSpans, e.ToolCallID)
			}
			st.mu.Unlock()
		}
	}
	if span == nil {
		span = trace.SpanFromContext(ctx)
	}
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(setFiniteFloat64(nil, "gen_ai.execute_tool.duration", msToSeconds(e.DurationMs))...)
	if e.Error != nil {
		RecordErrorOnSpan(span, e.Error)
	} else if e.Settings == nil || e.Settings.RecordOutputs {
		// TS: gen_ai.tool.call.result is an output (recordOutputs gate).
		if b, err := jsonstr.Marshal(e.Result); err == nil {
			span.SetAttributes(attribute.String("gen_ai.tool.call.result", string(b)))
		}
	}
	span.End()
}

// toolCallArgumentsJSON returns the JSON text TS records as
// gen_ai.tool.call.arguments (JSON.stringify of the parsed tool input).
func toolCallArgumentsJSON(tc types.ToolCallContent) (string, bool) {
	if tc.Arguments != nil {
		b, err := jsonstr.Marshal(tc.Arguments)
		return string(b), err == nil
	}
	if tc.Input == "" {
		return "", false
	}
	if json.Valid([]byte(tc.Input)) {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(tc.Input)); err == nil {
			return compact.String(), true
		}
	}
	b, err := jsonstr.Marshal(tc.Input)
	return string(b), err == nil
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
	if e.FinishReason == string(types.FinishReasonError) {
		stepSpan.SetStatus(codes.Error, "")
	}
	// TS onStepEnd records only the supplemental attributes on the step span;
	// the response id/model, finish reasons and token usage live on the
	// nested chat span (and the root), so repeating them here would
	// double-count tokens in aggregations.
	var attrs []attribute.KeyValue
	if i.opts.Usage {
		attrs = append(attrs, detailedUsageAttrs(e.Usage)...)
	}
	// Gated only by opts.ProviderMetadata (not recordOutputs) — see the
	// identical comment in OnLanguageModelCallEnd.
	if i.opts.ProviderMetadata && e.ProviderMetadata != nil {
		if b, err := jsonstr.Marshal(e.ProviderMetadata); err == nil {
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
	// H5: clear the recorded step span so OnAbort/OnError (which also close
	// state.stepSpan if it's still set) don't try to end it a second time.
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			if st.stepSpan == stepSpan {
				st.stepSpan = nil
			}
			st.mu.Unlock()
		}
	}
}

// OnFinish is the deprecated compatibility alias for OnEnd.
func (i OpenTelemetry) OnFinish(ctx context.Context, e TelemetryFinishEvent) { i.OnEnd(ctx, e) }

// OnEnd sets final output attributes on the root span and ends it. It omits
// gen_ai.usage.input_tokens for embed/embedMany operations to avoid the
// double count fixed in c0a42bc — that count already lives on the
// embeddings request span from OnEmbedEnd.
func (i OpenTelemetry) OnEnd(ctx context.Context, e TelemetryFinishEvent) {
	// H5: resolve OUR OWN root span by CallID rather than
	// trace.SpanFromContext(ctx), which — with a second integration also
	// registered — would silently resolve to that other integration's span
	// and leave ours never ended.
	span := genAIRootSpanFor(e.CallID, ctx)
	if !span.IsRecording() {
		return
	}
	if e.FinishReason == string(types.FinishReasonError) {
		span.SetStatus(codes.Error, "")
	}
	if e.OperationType == "ai.generateSpeech" || e.OperationType == "ai.transcribe" || e.OperationType == "ai.streamTranscribe" {
		// onAudioOperationEnd (TS open-telemetry.ts): a completely different
		// attribute set from every other operation — no
		// gen_ai.response.finish_reasons (audio operations have no finish
		// reason) and no token-based gen_ai.usage.* (appendGenAIUsageAttrs).
		i.onAudioOperationEnd(span, e)
		span.End()
		genAIDeleteState(e.CallID)
		return
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	switch e.OperationType {
	case "ai.embed", "ai.embedMany":
		i.onEmbedOperationEnd(span, e, recordOutputs)
	case "ai.rerank":
		// TS onRerankOperationEnd only ends the root span.
	case "ai.generateObject", "ai.streamObject":
		i.onObjectOperationEnd(span, e, recordOutputs)
	default:
		i.onGenerateEnd(span, e, recordOutputs)
	}
	span.End()
	genAIDeleteState(e.CallID)
}

// onGenerateEnd mirrors TS onGenerateEnd (generateText/streamText): finish
// reason, token usage, gen_ai.output.messages, and the supplemental
// providerMetadata/usage details.
func (i OpenTelemetry) onGenerateEnd(span trace.Span, e TelemetryFinishEvent, recordOutputs bool) {
	attrs := []attribute.KeyValue{
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}),
	}
	attrs = append(attrs, baseUsageAttrs(e.Usage, true)...)
	if recordOutputs {
		if b, err := jsonstr.Marshal(formatOutputMessagesFromFinish(e)); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.output.messages", string(b)))
		}
	}
	attrs = append(attrs, i.endSupplementalAttrs(e)...)
	span.SetAttributes(attrs...)
}

// onObjectOperationEnd mirrors TS onObjectOperationEnd (generateObject/
// streamObject): like onGenerateEnd without the cache-creation tokens, and
// gen_ai.output.messages carrying the serialized object when there is one.
func (i OpenTelemetry) onObjectOperationEnd(span trace.Span, e TelemetryFinishEvent, recordOutputs bool) {
	attrs := []attribute.KeyValue{
		attribute.StringSlice("gen_ai.response.finish_reasons", []string{e.FinishReason}),
	}
	attrs = append(attrs, baseUsageAttrs(e.Usage, false)...)
	if recordOutputs && e.Object != nil {
		if objectJSON, err := jsonstr.Marshal(e.Object); err == nil {
			if b, err := jsonstr.Marshal(formatObjectOutputMessages(string(objectJSON), e.FinishReason)); err == nil {
				attrs = append(attrs, attribute.String("gen_ai.output.messages", string(b)))
			}
		}
	}
	attrs = append(attrs, i.endSupplementalAttrs(e)...)
	span.SetAttributes(attrs...)
}

// onEmbedOperationEnd mirrors TS onEmbedOperationEnd: only the embedding
// values (ai.embedding / ai.embeddings), no usage or finish reason — the
// token count lives on the nested embeddings span.
func (i OpenTelemetry) onEmbedOperationEnd(span trace.Span, e TelemetryFinishEvent, recordOutputs bool) {
	if !i.opts.Embedding || !recordOutputs || e.Embedding == nil {
		return
	}
	switch v := e.Embedding.(type) {
	case [][]float64:
		if len(v) > 0 {
			span.SetAttributes(attribute.StringSlice("ai.embeddings", jsonStringifyEach(v)))
		}
	case []float64:
		if b, err := jsonstr.Marshal(v); err == nil {
			span.SetAttributes(attribute.String("ai.embedding", string(b)))
		}
	}
}

// endSupplementalAttrs returns the supplemental attributes TS adds to a
// finished root span: provider metadata (OpenTelemetryOptions.ProviderMetadata)
// and the detailed usage breakdown (OpenTelemetryOptions.Usage).
func (i OpenTelemetry) endSupplementalAttrs(e TelemetryFinishEvent) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if i.opts.ProviderMetadata && e.ProviderMetadata != nil {
		if b, err := jsonstr.Marshal(e.ProviderMetadata); err == nil {
			attrs = append(attrs, attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
	if i.opts.Usage {
		attrs = append(attrs, detailedUsageAttrs(e.Usage)...)
	}
	return attrs
}

// formatOutputMessagesFromFinish builds the root span's gen_ai.output.messages
// the way TS onGenerateEnd does: final-step reasoning and text, then the tool
// calls and results and the files of every step.
func formatOutputMessagesFromFinish(e TelemetryFinishEvent) []semConvOutputMessage {
	parts := make([]semConvPart, 0, len(e.ReasoningParts)+len(e.ToolCalls)+len(e.ToolResults)+len(e.Files)+1)
	for _, r := range e.ReasoningParts {
		if r.Text != "" {
			parts = append(parts, semConvPart{"type": "reasoning", "content": r.Text})
		}
	}
	if e.Text != "" {
		parts = append(parts, semConvPart{"type": "text", "content": e.Text})
	}
	for _, tc := range e.ToolCalls {
		parts = append(parts, semConvPart{
			"type":      "tool_call",
			"id":        nullableString(tc.ID),
			"name":      tc.ToolName,
			"arguments": tc.Arguments,
		})
	}
	for _, tr := range e.ToolResults {
		if tr.Error != nil || tr.Preliminary {
			continue
		}
		parts = append(parts, semConvPart{
			"type":     "tool_call_response",
			"id":       nullableString(tr.ToolCallID),
			"response": tr.Result,
		})
	}
	for _, f := range e.Files {
		parts = append(parts, generatedFileToSemConv(f))
	}
	return []semConvOutputMessage{{
		Role:         "assistant",
		Parts:        parts,
		FinishReason: mapFinishReasonSemConv(e.FinishReason),
	}}
}

// onAudioOperationEnd sets the final attributes for "ai.generateSpeech"/
// "ai.transcribe"/"ai.streamTranscribe" on the root span, mirroring TS's
// onAudioOperationEnd (open-telemetry.ts): ai.request.audio.* for the
// transcribe/streamTranscribe input audio (input-gated), ai.response.text/
// gen_ai.output.messages for the transcript and ai.response.audio.* for the
// generated speech audio (both output-gated), gen_ai.usage.* (unconditional,
// via getProviderUsageAttributes), ai.response.usage (gated by
// OpenTelemetryOptions.Usage), and ai.response.providerMetadata (gated by
// OpenTelemetryOptions.ProviderMetadata, not recordOutputs, matching every
// other operation's providerMetadata attribute in this file).
func (i OpenTelemetry) onAudioOperationEnd(span trace.Span, e TelemetryFinishEvent) {
	isSpeech := e.OperationType == "ai.generateSpeech"
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	if !isSpeech && recordInputs {
		if e.AudioByteLength != nil {
			span.SetAttributes(attribute.Int64("ai.request.audio.size", *e.AudioByteLength))
		}
		if e.AudioMediaType != "" {
			span.SetAttributes(attribute.String("ai.request.audio.media_type", e.AudioMediaType))
		}
	}
	if recordOutputs {
		// TS: transcript = 'segments' in event ? event.text : undefined, so a
		// transcription always records its (possibly empty) text and speech
		// never does.
		if !isSpeech {
			span.SetAttributes(attribute.String("ai.response.text", e.Text))
			// A plain map, not semConvOutputMessage: TS's inline JSON for
			// this attribute has no finish_reason key (unlike the shared
			// chat-response formatter), since audio operations have no
			// finish reason at all.
			if b, err := jsonstr.Marshal([]map[string]interface{}{{
				"role":  "assistant",
				"parts": []semConvPart{{"type": "text", "content": e.Text}},
			}}); err == nil {
				span.SetAttributes(attribute.String("gen_ai.output.messages", string(b)))
			}
		}
		if isSpeech {
			if e.AudioByteLength != nil {
				span.SetAttributes(attribute.Int64("ai.response.audio.size", *e.AudioByteLength))
			}
			if e.AudioMediaType != "" {
				span.SetAttributes(attribute.String("ai.response.audio.media_type", e.AudioMediaType))
			}
			if e.AudioFormat != "" {
				span.SetAttributes(attribute.String("ai.response.audio.format", e.AudioFormat))
			}
		}
	}
	if attrs := getProviderUsageAttributes(e.ProviderUsage, "gen_ai.usage"); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	if i.opts.Usage && e.ProviderUsage != nil {
		if b, err := jsonstr.Marshal(e.ProviderUsage); err == nil {
			span.SetAttributes(attribute.String("ai.response.usage", string(b)))
		}
	}
	if i.opts.ProviderMetadata && e.ProviderMetadata != nil {
		if b, err := jsonstr.Marshal(e.ProviderMetadata); err == nil {
			span.SetAttributes(attribute.String("ai.response.providerMetadata", string(b)))
		}
	}
}

// OnError records the error on the root span (with HTTP status when
// available) and ends it. It also defensively closes any nested decide
// span left open by OnDecisionModelCallStart for this CallID, since
// OnDecisionModelCallEnd is never notified on an error path.
func (i OpenTelemetry) OnError(ctx context.Context, e TelemetryErrorEvent) {
	if e.CallID != "" {
		if value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("decision", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				if e.Error != nil {
					RecordErrorOnSpan(entry.span, e.Error)
				}
				entry.span.End()
			}
		}
		if value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("languageModel", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				if e.Error != nil {
					RecordErrorOnSpan(entry.span, e.Error)
				}
				entry.span.End()
			}
		}
	}
	// H5: close any of OUR OWN still-open step/tool spans, resolved by
	// CallID — mirrors TS's onError stepSpan/toolSpans handling — otherwise a
	// step or tool span left open by an error that skipped
	// OnStepEnd/OnToolExecutionEnd would leak.
	if st := genAIState(e.CallID); st != nil {
		st.mu.Lock()
		stepSpan := st.stepSpan
		st.stepSpan = nil
		toolSpans := st.toolSpans
		st.toolSpans = nil
		embedSpans := st.embedSpans
		st.embedSpans = nil
		rerankSpan := st.rerankSpan
		st.rerankSpan = nil
		st.mu.Unlock()
		if stepSpan != nil && stepSpan.IsRecording() {
			if e.Error != nil {
				RecordErrorOnSpan(stepSpan, e.Error)
			}
			stepSpan.End()
		}
		for _, toolSpan := range toolSpans {
			if toolSpan.IsRecording() {
				if e.Error != nil {
					RecordErrorOnSpan(toolSpan, e.Error)
				}
				toolSpan.End()
			}
		}
		// H5: also close any still-open embed/rerank nested spans — otherwise
		// a doEmbed/doRerank span left open by a provider error that skipped
		// OnEmbedEnd/OnRerankEnd would leak.
		for _, embedSpan := range embedSpans {
			if embedSpan.IsRecording() {
				if e.Error != nil {
					RecordErrorOnSpan(embedSpan, e.Error)
				}
				embedSpan.End()
			}
		}
		if rerankSpan != nil && rerankSpan.IsRecording() {
			if e.Error != nil {
				RecordErrorOnSpan(rerankSpan, e.Error)
			}
			rerankSpan.End()
		}
	}
	span := genAIRootSpanFor(e.CallID, ctx)
	if !span.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(span, e.Error)
	}
	span.End()
	genAIDeleteState(e.CallID)
}

// OnAbort ends any in-flight model-call ("chat") span, any in-flight step/
// tool spans, and the root span. Mirrors TS onAbort's inferenceSpan/stepSpan
// handling (legacy-open-telemetry.ts / open-telemetry.ts): plain .end()
// calls, no error status recorded. H5: all are resolved by CallID from our
// own state (never trace.SpanFromContext(ctx)/ctx.Value(genAIStepSpanKey{})
// alone), so this closes the right spans even when another integration is
// also registered.
func (i OpenTelemetry) OnAbort(ctx context.Context, e TelemetryAbortEvent) {
	if e.CallID != "" {
		// TS onAbort also ends an in-flight decision model-call span.
		if value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("decision", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				entry.span.End()
			}
		}
		if value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("languageModel", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				entry.span.End()
			}
		}
	}
	if st := genAIState(e.CallID); st != nil {
		st.mu.Lock()
		stepSpan := st.stepSpan
		st.stepSpan = nil
		toolSpans := st.toolSpans
		st.toolSpans = nil
		embedSpans := st.embedSpans
		st.embedSpans = nil
		rerankSpan := st.rerankSpan
		st.rerankSpan = nil
		st.mu.Unlock()
		if stepSpan != nil && stepSpan.IsRecording() {
			stepSpan.End()
		}
		for _, toolSpan := range toolSpans {
			if toolSpan.IsRecording() {
				toolSpan.End()
			}
		}
		for _, embedSpan := range embedSpans {
			if embedSpan.IsRecording() {
				embedSpan.End()
			}
		}
		if rerankSpan != nil && rerankSpan.IsRecording() {
			rerankSpan.End()
		}
	} else if stepSpan, ok := ctx.Value(genAIStepSpanKey{}).(trace.Span); ok && stepSpan.IsRecording() {
		stepSpan.End()
	}
	span := genAIRootSpanFor(e.CallID, ctx)
	if !span.IsRecording() {
		return
	}
	span.End()
	genAIDeleteState(e.CallID)
}

// OnStepError closes a still-open step span and the nested model-call
// ("chat") span when the provider call itself failed, so neither OnStepEnd
// nor OnLanguageModelCallEnd will ever fire for them — H4 item 2's "span
// leak on provider error" fix. Mirrors TS onError's stepSpan/inferenceSpan
// handling: recordErrorOnSpan + end when e.Error is set (provider error),
// or a plain end (mirroring onAbort) when it's nil.
func (i OpenTelemetry) OnStepError(ctx context.Context, e TelemetryErrorEvent) {
	if e.CallID != "" {
		if value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("languageModel", e.CallID)); ok {
			if entry, ok := value.(otelSpanEntry); ok && entry.span.IsRecording() {
				if e.Error != nil {
					RecordErrorOnSpan(entry.span, e.Error)
				}
				entry.span.End()
			}
		}
	}
	stepSpan, ok := ctx.Value(genAIStepSpanKey{}).(trace.Span)
	if !ok || !stepSpan.IsRecording() {
		return
	}
	if e.Error != nil {
		RecordErrorOnSpan(stepSpan, e.Error)
	}
	stepSpan.End()
	if e.CallID != "" {
		if st := genAIState(e.CallID); st != nil {
			st.mu.Lock()
			if st.stepSpan == stepSpan {
				st.stepSpan = nil
			}
			st.mu.Unlock()
		}
	}
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

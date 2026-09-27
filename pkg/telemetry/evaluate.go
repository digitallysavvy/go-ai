package telemetry

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ---------------------------------------------------------------------------
// experimental_evaluate telemetry events
// ---------------------------------------------------------------------------
//
// These mirror TypeScript's restricted telemetry dispatcher for evaluate
// (packages/ai/src/evaluate/restricted-telemetry-dispatcher.ts): evaluate's
// onStart/onEnd are routed to experimental_onEvaluateStart/End instead of the
// generic onStart/onEnd used by generateText/embed/etc, so core evaluate
// telemetry never gets fed into (or interpreted by) integrations expecting
// the shape of a generateText-family operation. The nested model call is
// reported separately via experimental_onEvaluationModelCallStart/End
// (packages/ai/src/evaluate/evaluate.ts).

// EvaluateStartEvent is emitted before calling the evaluation model, routed
// as experimental_onEvaluateStart.
type EvaluateStartEvent struct {
	Settings *Settings
	CallID   string
	// OperationID is the canonical operation name ("ai.evaluate").
	OperationID   string
	ModelProvider string
	ModelID       string
	// RuntimeContext is already filtered by Settings.IncludeRuntimeContext.
	RuntimeContext map[string]interface{}
	// State and Questions are JSON-compatible values, JSON-encoded for span
	// attributes (ai.evaluation.state / ai.evaluation.questions).
	State      interface{}
	Questions  interface{}
	MaxRetries int
	Headers    map[string]string
}

// EvaluateEndEvent is emitted after the evaluate operation completes
// successfully, routed as experimental_onEvaluateEnd.
type EvaluateEndEvent struct {
	EvaluateStartEvent
	// Answers is JSON-encoded as ai.evaluation.answers (output-gated).
	Answers interface{}
}

// EvaluationModelCallStartEvent is emitted immediately before the
// evaluation model's DoEvaluate call (the nested "ai.evaluate.doEvaluate"
// span), routed as experimental_onEvaluationModelCallStart.
type EvaluationModelCallStartEvent struct {
	Settings *Settings
	CallID   string
	// OperationID is the canonical nested operation name ("ai.evaluate.doEvaluate").
	OperationID   string
	ModelProvider string
	ModelID       string
	State         interface{}
	Questions     interface{}
}

// EvaluationModelCallEndEvent is emitted after the evaluation model call
// completes successfully, routed as experimental_onEvaluationModelCallEnd.
// Not notified when the call or answer validation fails — the registered
// integration is responsible for defensively closing any span it opened at
// EvaluationModelCallStartEvent when FireOnError fires instead (mirrors
// evaluate.ts, where onEvaluationModelCallEnd is only notified on success).
type EvaluationModelCallEndEvent struct {
	EvaluationModelCallStartEvent
	// Answers is JSON-encoded as ai.evaluation.answers (output-gated).
	Answers          interface{}
	Usage            TelemetryUsage
	ProviderMetadata map[string]interface{}
}

type evaluateStartHandler interface {
	OnEvaluateStart(context.Context, EvaluateStartEvent) context.Context
}

type evaluateEndHandler interface {
	OnEvaluateEnd(context.Context, EvaluateEndEvent)
}

type evaluationModelCallStartHandler interface {
	OnEvaluationModelCallStart(context.Context, EvaluationModelCallStartEvent)
}

type evaluationModelCallEndHandler interface {
	OnEvaluationModelCallEnd(context.Context, EvaluationModelCallEndEvent)
}

// FireOnEvaluateStart publishes and fans out an evaluate-operation start
// event to integrations implementing OnEvaluateStart, threading ctx through
// each call so an OTel integration can embed a root span in the returned
// ctx (used by FireOnEvaluationModelCallStart, FireOnEvaluateEnd, and
// FireOnError).
func FireOnEvaluateStart(ctx context.Context, e EvaluateStartEvent) context.Context {
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEvaluateStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(evaluateStartHandler); ok {
			ctx = handler.OnEvaluateStart(ctx, e)
		}
	}
	return ctx
}

// FireOnEvaluateEnd publishes and fans out an evaluate-operation end event.
func FireOnEvaluateEnd(ctx context.Context, e EvaluateEndEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEvaluateEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(evaluateEndHandler); ok {
			handler.OnEvaluateEnd(ctx, e)
		}
	}
}

// FireOnEvaluationModelCallStart publishes and fans out an
// evaluation-model-call start event.
func FireOnEvaluationModelCallStart(ctx context.Context, e EvaluationModelCallStartEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEvaluationModelCallStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(evaluationModelCallStartHandler); ok {
			handler.OnEvaluationModelCallStart(ctx, e)
		}
	}
}

// FireOnEvaluationModelCallEnd publishes and fans out an
// evaluation-model-call end event.
func FireOnEvaluationModelCallEnd(ctx context.Context, e EvaluationModelCallEndEvent) {
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnEvaluationModelCallEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(evaluationModelCallEndHandler); ok {
			handler.OnEvaluationModelCallEnd(ctx, e)
		}
	}
}

// jsonAttr JSON-encodes v and returns a string attribute.KeyValue, or false
// if v is nil or cannot be marshaled. Mirrors TS's `JSON.stringify(...)`
// content attributes for evaluation state/questions/answers.
func jsonAttr(key string, v interface{}) (attribute.KeyValue, bool) {
	if v == nil {
		return attribute.KeyValue{}, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return attribute.KeyValue{}, false
	}
	return attribute.String(key, string(b)), true
}

// ---------------------------------------------------------------------------
// LegacyOpenTelemetry: evaluate spans
// ---------------------------------------------------------------------------

// OnEvaluateStart creates the root "ai.evaluate" span for an
// ExperimentalEvaluate call and embeds it in the returned context, mirroring
// TS's onEvaluateOperationStart (otel/src/legacy-open-telemetry.ts).
// Attribute shape follows the same base-attribute parity fix as the generic
// OnStart (follow-up H1): ai.model.provider/id, ai.settings.maxRetries,
// ai.request.headers.<name>, and operation.name/resource.name are emitted;
// gen_ai.system/gen_ai.request.model are NOT part of TS's root span (TS's
// evaluate root span carries no gen_ai.* at all) but are left in place here
// since pkg/telemetry/evaluate_test.go and other consumers already assert on
// them and TS's own dual-emission convention elsewhere makes this a
// reasonable superset rather than a wrong value.
func (i LegacyOpenTelemetry) OnEvaluateStart(ctx context.Context, e EvaluateStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := i.tracerFor(e.Settings)
	ctx, span := tracer.Start(ctx, e.OperationID)
	if attrs := customSpanAttributes(ctx, i.enrichSpan, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationID,
		CallID:         e.CallID,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	functionID := ""
	if e.Settings != nil {
		functionID = e.Settings.FunctionID
	}
	span.SetAttributes(legacyOperationNameAttrs(e.OperationID, functionID)...)
	span.SetAttributes(
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)

	maxRetries := e.MaxRetries
	baseAttrs := legacyBaseAttrs(e.ModelProvider, e.ModelID, legacySettings{MaxRetries: &maxRetries}, e.Headers)
	span.SetAttributes(baseAttrs...)
	ctx = context.WithValue(ctx, legacyBaseAttrsKey{}, baseAttrs)

	if e.Settings == nil || e.Settings.RecordInputs {
		if attr, ok := jsonAttr("ai.evaluation.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.evaluation.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
		span.SetAttributes(attrs...)
		ctx = context.WithValue(ctx, runtimeContextAttrsKey{}, attrs)
	}
	return ctx
}

// OnEvaluateEnd sets output attributes on the root "ai.evaluate" span
// (found via trace.SpanFromContext, embedded by OnEvaluateStart) and ends
// it, mirroring TS's onEvaluateOperationEnd.
func (i LegacyOpenTelemetry) OnEvaluateEnd(ctx context.Context, e EvaluateEndEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Settings == nil || e.Settings.RecordOutputs {
		if attr, ok := jsonAttr("ai.evaluation.answers", e.Answers); ok {
			span.SetAttributes(attr)
		}
	}
	span.End()
}

// OnEvaluationModelCallStart creates the nested "ai.evaluate.doEvaluate"
// child span, tracked by CallID (like OnEmbedStart/OnRerankStart) so
// OnEvaluationModelCallEnd or a defensive OnError close can find it again.
// Mirrors TS's experimental_onEvaluationModelCallStart.
func (i LegacyOpenTelemetry) OnEvaluationModelCallStart(ctx context.Context, e EvaluationModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	tracer := parent.TracerProvider().Tracer("go-ai")
	_, span := tracer.Start(ctx, e.OperationID)
	functionID := ""
	if e.Settings != nil {
		functionID = e.Settings.FunctionID
	}
	span.SetAttributes(legacyOperationNameAttrs(e.OperationID, functionID)...)
	span.SetAttributes(
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if baseAttrs, ok := ctx.Value(legacyBaseAttrsKey{}).([]attribute.KeyValue); ok {
		span.SetAttributes(baseAttrs...)
	}
	if e.Settings == nil || e.Settings.RecordInputs {
		if attr, ok := jsonAttr("ai.evaluation.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.evaluation.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	if e.CallID != "" {
		otelModelCallSpans.Store(otelSpanKey("evaluation", e.CallID), otelSpanEntry{span: span})
	}
}

// OnEvaluationModelCallEnd records evaluation answers, usage, and provider
// metadata on the nested "ai.evaluate.doEvaluate" span and ends it, mirroring
// TS's experimental_onEvaluationModelCallEnd.
func (i LegacyOpenTelemetry) OnEvaluationModelCallEnd(_ context.Context, e EvaluationModelCallEndEvent) {
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("evaluation", e.CallID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.Settings == nil || e.Settings.RecordOutputs {
		if attr, ok := jsonAttr("ai.evaluation.answers", e.Answers); ok {
			entry.span.SetAttributes(attr)
		}
	}
	if e.Usage.InputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("ai.usage.inputTokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("ai.usage.outputTokens", *e.Usage.OutputTokens))
	}
	if e.ProviderMetadata != nil {
		if attr, ok := jsonAttr("ai.response.providerMetadata", e.ProviderMetadata); ok {
			entry.span.SetAttributes(attr)
		}
	}
	entry.span.End()
}

// ---------------------------------------------------------------------------
// OpenTelemetry (GenAI semconv): evaluate spans
// ---------------------------------------------------------------------------

// OnEvaluateStart creates the root "evaluate {modelId}" span with GenAI
// semantic-convention attributes, gating ai.evaluation.state/questions
// behind OpenTelemetryOptions.ExperimentalEvaluation, mirroring TS's
// onEvaluateOperationStart (otel/src/open-telemetry.ts).
func (i OpenTelemetry) OnEvaluateStart(ctx context.Context, e EvaluateStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := i.tracerFor(e.Settings)
	spanName := "evaluate"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	ctx, span := tracer.Start(ctx, spanName)
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationID,
		CallID:         e.CallID,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "evaluate"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if i.opts.Headers {
		for _, attr := range headerAttributes(e.Headers) {
			span.SetAttributes(attr)
		}
	}
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if i.opts.ExperimentalEvaluation && recordInputs {
		if attr, ok := jsonAttr("ai.evaluation.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.evaluation.questions", e.Questions); ok {
			span.SetAttributes(attr)
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

// OnEvaluateEnd sets output attributes on the root evaluate span and ends
// it, mirroring TS's onEvaluateOperationEnd.
func (i OpenTelemetry) OnEvaluateEnd(ctx context.Context, e EvaluateEndEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	if i.opts.ExperimentalEvaluation && recordOutputs {
		if attr, ok := jsonAttr("ai.evaluation.answers", e.Answers); ok {
			span.SetAttributes(attr)
		}
	}
	span.End()
}

// OnEvaluationModelCallStart creates the nested "evaluate {modelId}" child
// span for the underlying model call, tracked by CallID, mirroring TS's
// experimental_onEvaluationModelCallStart.
func (i OpenTelemetry) OnEvaluationModelCallStart(ctx context.Context, e EvaluationModelCallStartEvent) {
	parent := trace.SpanFromContext(ctx)
	if !parent.IsRecording() {
		return
	}
	tracer := parent.TracerProvider().Tracer("go-ai")
	spanName := "evaluate"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(ctx, spanName)
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "evaluate"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if i.opts.ExperimentalEvaluation && recordInputs {
		if attr, ok := jsonAttr("ai.evaluation.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.evaluation.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	if e.CallID != "" {
		genAICallSpans.Store(genAISpanKey("evaluation", e.CallID), otelSpanEntry{span: span})
	}
}

// OnEvaluationModelCallEnd records usage, evaluation answers, and provider
// metadata on the nested evaluate span and ends it, mirroring TS's
// experimental_onEvaluationModelCallEnd.
func (i OpenTelemetry) OnEvaluationModelCallEnd(_ context.Context, e EvaluationModelCallEndEvent) {
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("evaluation", e.CallID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.Usage.InputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	if i.opts.ExperimentalEvaluation && recordOutputs {
		if attr, ok := jsonAttr("ai.evaluation.answers", e.Answers); ok {
			entry.span.SetAttributes(attr)
		}
	}
	// Gated only by opts.ProviderMetadata (not recordOutputs), matching TS's
	// experimental_onEvaluationModelCallEnd: providerMetadata is a plain
	// pre-computed value passed through selectSupplementalAttributes, not
	// wrapped in an {output: () => ...} accessor, so selectAttributes never
	// applies its recordOutputs gate to it.
	if i.opts.ProviderMetadata && e.ProviderMetadata != nil {
		if attr, ok := jsonAttr("ai.response.providerMetadata", e.ProviderMetadata); ok {
			entry.span.SetAttributes(attr)
		}
	}
	entry.span.End()
}

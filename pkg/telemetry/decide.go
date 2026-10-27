package telemetry

import (
	"context"
	"github.com/digitallysavvy/go-ai/pkg/internal/sdkversion"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// ---------------------------------------------------------------------------
// experimental_decide telemetry events
// ---------------------------------------------------------------------------
//
// These mirror TypeScript's restricted telemetry dispatcher for decide
// (packages/ai/src/decide/restricted-telemetry-dispatcher.ts): decide's
// onStart/onEnd are routed to experimental_onDecideStart/End instead of the
// generic onStart/onEnd used by generateText/embed/etc, so core decide
// telemetry never gets fed into (or interpreted by) integrations expecting
// the shape of a generateText-family operation. The nested model call is
// reported separately via experimental_onDecisionModelCallStart/End
// (packages/ai/src/decide/decide.ts).

// DecideStartEvent is emitted before calling the decision model, routed
// as experimental_onDecideStart.
type DecideStartEvent struct {
	Settings *Settings
	CallID   string
	// OperationID is the canonical operation name ("ai.decide").
	OperationID   string
	ModelProvider string
	ModelID       string
	// RuntimeContext is already filtered by Settings.IncludeRuntimeContext.
	RuntimeContext map[string]interface{}
	// State and Questions are JSON-compatible values, JSON-encoded for span
	// attributes (ai.decision.state / ai.decision.questions).
	State      interface{}
	Questions  interface{}
	MaxRetries int
	Headers    map[string]string

	// AIVersion is the version of the Go AI SDK that emitted the event. The
	// Fire* helpers set it, overwriting any caller value. Mirrors the TS
	// `aiVersion` field the telemetry dispatcher adds to every event (TS #23958).
	AIVersion string
}

// DecideEndEvent is emitted after the decide operation completes
// successfully, routed as experimental_onDecideEnd.
type DecideEndEvent struct {
	DecideStartEvent
	// Answers is JSON-encoded as ai.decision.answers (output-gated).
	Answers interface{}

	// AIVersion is the version of the Go AI SDK that emitted the event. The
	// Fire* helpers set it, overwriting any caller value. Mirrors the TS
	// `aiVersion` field the telemetry dispatcher adds to every event (TS #23958).
	AIVersion string
}

// DecisionModelCallStartEvent is emitted immediately before the
// decision model's DoDecide call (the nested "ai.decide.doDecide"
// span), routed as experimental_onDecisionModelCallStart.
type DecisionModelCallStartEvent struct {
	Settings *Settings
	CallID   string
	// OperationID is the canonical nested operation name ("ai.decide.doDecide").
	OperationID   string
	ModelProvider string
	ModelID       string
	State         interface{}
	Questions     interface{}

	// AIVersion is the version of the Go AI SDK that emitted the event. The
	// Fire* helpers set it, overwriting any caller value. Mirrors the TS
	// `aiVersion` field the telemetry dispatcher adds to every event (TS #23958).
	AIVersion string
}

// DecisionModelCallEndEvent is emitted after the decision model call
// completes successfully, routed as experimental_onDecisionModelCallEnd.
// Not notified when the call or answer validation fails — the registered
// integration is responsible for defensively closing any span it opened at
// DecisionModelCallStartEvent when FireOnError fires instead (mirrors
// decide.ts, where onDecisionModelCallEnd is only notified on success).
type DecisionModelCallEndEvent struct {
	DecisionModelCallStartEvent
	// Answers is JSON-encoded as ai.decision.answers (output-gated).
	Answers          interface{}
	Usage            TelemetryUsage
	ProviderMetadata map[string]interface{}
	// ResponseID and ResponseModelID are the provider-reported response ID
	// and resolved model ID, when the model reported them. Recorded by the
	// GenAI OpenTelemetry integration as gen_ai.response.id and
	// gen_ai.response.model.
	ResponseID      string
	ResponseModelID string

	// AIVersion is the version of the Go AI SDK that emitted the event. The
	// Fire* helpers set it, overwriting any caller value. Mirrors the TS
	// `aiVersion` field the telemetry dispatcher adds to every event (TS #23958).
	AIVersion string
}

type decideStartHandler interface {
	OnDecideStart(context.Context, DecideStartEvent) context.Context
}

type decideEndHandler interface {
	OnDecideEnd(context.Context, DecideEndEvent)
}

type decisionModelCallStartHandler interface {
	OnDecisionModelCallStart(context.Context, DecisionModelCallStartEvent)
}

type decisionModelCallEndHandler interface {
	OnDecisionModelCallEnd(context.Context, DecisionModelCallEndEvent)
}

// Deprecated pre-rename event names and handler interfaces. An integration
// implementing a deprecated handler keeps working; when it implements both
// the current and the deprecated handler, only the current one is called
// (mirrors TS's `integration[key] ?? integration[deprecatedKey]`).

// EvaluateStartEvent is an alias of DecideStartEvent.
//
// Deprecated: Use DecideStartEvent instead.
type EvaluateStartEvent = DecideStartEvent

// EvaluateEndEvent is an alias of DecideEndEvent.
//
// Deprecated: Use DecideEndEvent instead.
type EvaluateEndEvent = DecideEndEvent

// EvaluationModelCallStartEvent is an alias of DecisionModelCallStartEvent.
//
// Deprecated: Use DecisionModelCallStartEvent instead.
type EvaluationModelCallStartEvent = DecisionModelCallStartEvent

// EvaluationModelCallEndEvent is an alias of DecisionModelCallEndEvent.
//
// Deprecated: Use DecisionModelCallEndEvent instead.
type EvaluationModelCallEndEvent = DecisionModelCallEndEvent

// evaluateStartHandler is the deprecated form of decideStartHandler.
type evaluateStartHandler interface {
	OnEvaluateStart(context.Context, DecideStartEvent) context.Context
}

// evaluateEndHandler is the deprecated form of decideEndHandler.
type evaluateEndHandler interface {
	OnEvaluateEnd(context.Context, DecideEndEvent)
}

// evaluationModelCallStartHandler is the deprecated form of
// decisionModelCallStartHandler.
type evaluationModelCallStartHandler interface {
	OnEvaluationModelCallStart(context.Context, DecisionModelCallStartEvent)
}

// evaluationModelCallEndHandler is the deprecated form of
// decisionModelCallEndHandler.
type evaluationModelCallEndHandler interface {
	OnEvaluationModelCallEnd(context.Context, DecisionModelCallEndEvent)
}

// FireOnDecideStart publishes and fans out a decide-operation start
// event to integrations implementing OnDecideStart. Each integration is
// given the SAME base ctx (H5 — see the identical rationale on FireOnStart in
// registry.go) rather than the previous integration's result, so two
// registered integrations' root spans come out as siblings and each resolves
// its own root/nested spans from its own callId-keyed state rather than
// trace.SpanFromContext(ctx).
func FireOnDecideStart(ctx context.Context, e DecideStartEvent) context.Context {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	PublishDiagnostic(ctx, DiagnosticEventOnDecideStart, e)
	result := ctx
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(decideStartHandler); ok {
			callIntegration(func() { result = handler.OnDecideStart(ctx, e) })
		} else if handler, ok := integration.(evaluateStartHandler); ok {
			callIntegration(func() { result = handler.OnEvaluateStart(ctx, e) })
		}
	}
	return result
}

// FireOnDecideEnd publishes and fans out a decide-operation end event.
func FireOnDecideEnd(ctx context.Context, e DecideEndEvent) {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnDecideEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(decideEndHandler); ok {
			callIntegration(func() { handler.OnDecideEnd(ctx, e) })
		} else if handler, ok := integration.(evaluateEndHandler); ok {
			callIntegration(func() { handler.OnEvaluateEnd(ctx, e) })
		}
	}
}

// FireOnDecisionModelCallStart publishes and fans out a
// decision-model-call start event.
func FireOnDecisionModelCallStart(ctx context.Context, e DecisionModelCallStartEvent) {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnDecisionModelCallStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(decisionModelCallStartHandler); ok {
			callIntegration(func() { handler.OnDecisionModelCallStart(ctx, e) })
		} else if handler, ok := integration.(evaluationModelCallStartHandler); ok {
			callIntegration(func() { handler.OnEvaluationModelCallStart(ctx, e) })
		}
	}
}

// FireOnDecisionModelCallEnd publishes and fans out a
// decision-model-call end event.
func FireOnDecisionModelCallEnd(ctx context.Context, e DecisionModelCallEndEvent) {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return
	}
	PublishDiagnostic(ctx, DiagnosticEventOnDecisionModelCallEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(decisionModelCallEndHandler); ok {
			callIntegration(func() { handler.OnDecisionModelCallEnd(ctx, e) })
		} else if handler, ok := integration.(evaluationModelCallEndHandler); ok {
			callIntegration(func() { handler.OnEvaluationModelCallEnd(ctx, e) })
		}
	}
}

// jsonAttr JSON-encodes v and returns a string attribute.KeyValue, or false
// if v is nil or cannot be marshaled. Mirrors TS's `JSON.stringify(...)`
// content attributes for decision state/questions/answers.
func jsonAttr(key string, v interface{}) (attribute.KeyValue, bool) {
	if v == nil {
		return attribute.KeyValue{}, false
	}
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return attribute.KeyValue{}, false
	}
	return attribute.String(key, string(b)), true
}

// ---------------------------------------------------------------------------
// LegacyOpenTelemetry: decide spans
// ---------------------------------------------------------------------------

// OnDecideStart creates the root "ai.decide" span for an
// ExperimentalDecide call and embeds it in the returned context, mirroring
// TS's onDecideOperationStart (otel/src/legacy-open-telemetry.ts).
// Attribute shape follows the same base-attribute parity fix as the generic
// OnStart (follow-up H1): ai.model.provider/id, ai.settings.maxRetries,
// ai.request.headers.<name>, and operation.name/resource.name are emitted.
// gen_ai.system/gen_ai.request.model are intentionally NOT set — TS's
// onDecideOperationStart carries no gen_ai.* attributes at all (only
// assembleOperationName + baseTelemetryAttributes + decision.state/
// questions).
func (i LegacyOpenTelemetry) OnDecideStart(ctx context.Context, e DecideStartEvent) context.Context {
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

	maxRetries := e.MaxRetries
	baseAttrs := legacyBaseAttrs(e.ModelProvider, e.ModelID, legacySettings{MaxRetries: &maxRetries}, e.Headers)
	span.SetAttributes(baseAttrs...)
	ctx = context.WithValue(ctx, legacyBaseAttrsKey{}, baseAttrs)

	if e.Settings == nil || e.Settings.RecordInputs {
		if attr, ok := jsonAttr("ai.decision.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.decision.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
		span.SetAttributes(attrs...)
		ctx = context.WithValue(ctx, runtimeContextAttrsKey{}, attrs)
	}
	// H5: record this integration's own root span by CallID, so
	// OnDecideEnd/OnDecisionModelCallStart can find it instead of relying
	// on trace.SpanFromContext(ctx), which only ever resolves the single
	// "current" span and would break with a second integration registered.
	if e.CallID != "" {
		st := legacyState(e.CallID)
		st.mu.Lock()
		st.rootSpan = span
		st.mu.Unlock()
	}
	return ctx
}

// OnDecideEnd sets output attributes on the root "ai.decide" span
// (resolved by CallID, embedded by OnDecideStart) and ends it, mirroring
// TS's onDecideOperationEnd.
func (i LegacyOpenTelemetry) OnDecideEnd(ctx context.Context, e DecideEndEvent) {
	span := legacyRootSpanFor(e.CallID, ctx)
	if !span.IsRecording() {
		return
	}
	if e.Settings == nil || e.Settings.RecordOutputs {
		if attr, ok := jsonAttr("ai.decision.answers", e.Answers); ok {
			span.SetAttributes(attr)
		}
	}
	span.End()
	legacyDeleteState(e.CallID)
}

// OnDecisionModelCallStart creates the nested "ai.decide.doDecide"
// child span, tracked by CallID (like OnEmbedStart/OnRerankStart) so
// OnDecisionModelCallEnd or a defensive OnError close can find it again.
// Mirrors TS's experimental_onDecisionModelCallStart — which, like
// onDecideOperationStart, carries no gen_ai.* attributes.
func (i LegacyOpenTelemetry) OnDecisionModelCallStart(ctx context.Context, e DecisionModelCallStartEvent) {
	// H5: resolve our own root span by CallID rather than
	// trace.SpanFromContext(ctx).
	parent := legacyRootSpanFor(e.CallID, ctx)
	if !parent.IsRecording() {
		return
	}
	tracer := genAIChildTracer(e.CallID, parent)
	_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), e.OperationID)
	functionID := ""
	if e.Settings != nil {
		functionID = e.Settings.FunctionID
	}
	span.SetAttributes(legacyOperationNameAttrs(e.OperationID, functionID)...)
	if baseAttrs, ok := ctx.Value(legacyBaseAttrsKey{}).([]attribute.KeyValue); ok {
		span.SetAttributes(baseAttrs...)
	}
	if e.Settings == nil || e.Settings.RecordInputs {
		if attr, ok := jsonAttr("ai.decision.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.decision.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	if e.CallID != "" {
		otelModelCallSpans.Store(otelSpanKey("decision", e.CallID), otelSpanEntry{span: span})
	}
}

// OnDecisionModelCallEnd records decision answers, usage, and provider
// metadata on the nested "ai.decide.doDecide" span and ends it, mirroring
// TS's experimental_onDecisionModelCallEnd.
func (i LegacyOpenTelemetry) OnDecisionModelCallEnd(_ context.Context, e DecisionModelCallEndEvent) {
	value, ok := otelModelCallSpans.LoadAndDelete(otelSpanKey("decision", e.CallID))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.Settings == nil || e.Settings.RecordOutputs {
		if attr, ok := jsonAttr("ai.decision.answers", e.Answers); ok {
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
// OpenTelemetry (GenAI semconv): decide spans
// ---------------------------------------------------------------------------

// OnDecideStart creates the root "decide {modelId}" span with GenAI
// semantic-convention attributes, gating ai.decision.state/questions
// behind OpenTelemetryOptions.ExperimentalDecision, mirroring TS's
// onDecideOperationStart (otel/src/open-telemetry.ts).
func (i OpenTelemetry) OnDecideStart(ctx context.Context, e DecideStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := i.tracerFor(e.Settings)
	spanName := "decide"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationID,
		CallID:         e.CallID,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "decide"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	// TS baseSupplementalAttributes (runtime context and headers), reused
	// by the nested model-call span.
	var baseSupp []attribute.KeyValue
	if i.opts.RuntimeContext {
		if attrs := runtimeContextAttributes(e.RuntimeContext); len(attrs) > 0 {
			baseSupp = append(baseSupp, attrs...)
			ctx = context.WithValue(ctx, genAIRuntimeContextKey{}, attrs)
		}
	}
	if i.opts.Headers {
		baseSupp = append(baseSupp, headerAttributes(e.Headers)...)
	}
	span.SetAttributes(baseSupp...)
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if i.opts.experimentalDecision() && recordInputs {
		if attr, ok := jsonAttr("ai.decision.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.decision.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	// H5: record this integration's own root span by CallID, so
	// OnDecideEnd/OnDecisionModelCallStart can find it instead of relying
	// on trace.SpanFromContext(ctx).
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

// OnDecideEnd sets output attributes on the root decide span (resolved
// by CallID) and ends it, mirroring TS's onDecideOperationEnd.
func (i OpenTelemetry) OnDecideEnd(ctx context.Context, e DecideEndEvent) {
	span := genAIRootSpanFor(e.CallID, ctx)
	if !span.IsRecording() {
		return
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	if i.opts.experimentalDecision() && recordOutputs {
		if attr, ok := jsonAttr("ai.decision.answers", e.Answers); ok {
			span.SetAttributes(attr)
		}
	}
	span.End()
	genAIDeleteState(e.CallID)
}

// OnDecisionModelCallStart creates the nested "decide {modelId}" child
// span for the underlying model call, tracked by CallID, mirroring TS's
// experimental_onDecisionModelCallStart.
func (i OpenTelemetry) OnDecisionModelCallStart(ctx context.Context, e DecisionModelCallStartEvent) {
	// H5: resolve our own root span by CallID rather than
	// trace.SpanFromContext(ctx).
	parent := genAIRootSpanFor(e.CallID, ctx)
	if !parent.IsRecording() {
		return
	}
	tracer := genAIChildTracer(e.CallID, parent)
	spanName := "decide"
	if e.ModelID != "" {
		spanName += " " + e.ModelID
	}
	_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), spanName, trace.WithSpanKind(trace.SpanKindClient))
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:      SpanTypeDecision,
		OperationType: e.OperationID,
		CallID:        e.CallID,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "decide"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	span.SetAttributes(genAIBaseSupplemental(e.CallID)...)
	recordInputs := e.Settings == nil || e.Settings.RecordInputs
	if i.opts.experimentalDecision() && recordInputs {
		if attr, ok := jsonAttr("ai.decision.state", e.State); ok {
			span.SetAttributes(attr)
		}
		if attr, ok := jsonAttr("ai.decision.questions", e.Questions); ok {
			span.SetAttributes(attr)
		}
	}
	if e.CallID != "" {
		genAICallSpans.Store(genAISpanKey("decision", e.CallID), otelSpanEntry{span: span})
	}
}

// OnDecisionModelCallEnd records usage, decision answers, and provider
// metadata on the nested decide span and ends it, mirroring TS's
// experimental_onDecisionModelCallEnd.
func (i OpenTelemetry) OnDecisionModelCallEnd(_ context.Context, e DecisionModelCallEndEvent) {
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("decision", e.CallID))
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
	if e.ResponseID != "" {
		entry.span.SetAttributes(attribute.String("gen_ai.response.id", e.ResponseID))
	}
	if e.ResponseModelID != "" {
		entry.span.SetAttributes(attribute.String("gen_ai.response.model", e.ResponseModelID))
	}
	recordOutputs := e.Settings == nil || e.Settings.RecordOutputs
	if i.opts.experimentalDecision() && recordOutputs {
		if attr, ok := jsonAttr("ai.decision.answers", e.Answers); ok {
			entry.span.SetAttributes(attr)
		}
	}
	// Gated only by opts.ProviderMetadata (not recordOutputs), matching TS's
	// experimental_onDecisionModelCallEnd: providerMetadata is a plain
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

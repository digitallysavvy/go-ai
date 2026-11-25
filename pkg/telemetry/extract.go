package telemetry

import (
	"context"
	"net/url"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/internal/sdkversion"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ---------------------------------------------------------------------------
// experimental_extract telemetry events (TS 15bfd6b262)
// ---------------------------------------------------------------------------
//
// These mirror TS packages/ai/src/extract/extract-events.ts and its
// restricted telemetry dispatcher: extract's start and end are routed to
// experimental_onExtractStart / experimental_onExtractEnd, not to the
// generic OnStart / OnEnd, and the nested model call is reported by
// experimental_onExtractionModelCallStart / End. Only the GenAI
// OpenTelemetry integration records extract spans, as in TS.

// ExtractStartEvent is emitted before calling the extraction model, routed
// as experimental_onExtractStart.
//
// The Fire function removes what TS toTelemetryEvent removes: Headers and
// ProviderOptions are dropped, and the location of every file in State
// (a URL or provider reference) becomes "[redacted]".
type ExtractStartEvent struct {
	Settings *Settings
	CallID   string
	// OperationID is the canonical operation name ("ai.extract").
	OperationID   string
	ModelProvider string
	ModelID       string
	// RuntimeContext is already filtered by Settings.IncludeRuntimeContext.
	RuntimeContext map[string]interface{}
	// State is the shared state to extract from (a []provider.DecisionStatePart
	// as core passes it), JSON-encoded as ai.extraction.state.
	State interface{}
	// Fields maps field ID to field (JSON-compatible), JSON-encoded as
	// ai.extraction.fields.
	Fields interface{}
	// ReferenceDate is the YYYY-MM-DD date anchoring relative dates, or "".
	ReferenceDate   string
	MaxOutputTokens *int
	Reasoning       *types.ReasoningLevel
	MaxRetries      int
	Headers         map[string]string
	ProviderOptions map[string]interface{}

	// AIVersion is the version of the Go AI SDK that emitted the event. The
	// Fire* helpers set it, overwriting any caller value.
	AIVersion string
}

// ExtractEndEvent is emitted after the extract operation completes
// successfully, routed as experimental_onExtractEnd.
type ExtractEndEvent struct {
	ExtractStartEvent
	// Answers is JSON-encoded as ai.extraction.answers (output-gated).
	Answers          interface{}
	Usage            TelemetryUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         interface{}

	// AIVersion is the version of the Go AI SDK that emitted the event.
	AIVersion string
}

// ExtractionModelCallStartEvent is emitted immediately before the extraction
// model call (retries included), routed as
// experimental_onExtractionModelCallStart.
type ExtractionModelCallStartEvent struct {
	Settings *Settings
	CallID   string
	// OperationID is the canonical nested operation name
	// ("ai.extract.doExtract").
	OperationID   string
	ModelProvider string
	ModelID       string
	State         interface{}
	Fields        interface{}
	ReferenceDate string

	// AIVersion is the version of the Go AI SDK that emitted the event.
	AIVersion string
}

// ExtractionModelCallEndEvent is emitted after the extraction model response
// has been validated, routed as experimental_onExtractionModelCallEnd. Not
// notified when the call or validation fails: OnError closes the span then.
type ExtractionModelCallEndEvent struct {
	ExtractionModelCallStartEvent
	Answers          interface{}
	Usage            TelemetryUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	// ResponseID and ResponseModelID are the provider-reported response ID
	// and resolved model ID (gen_ai.response.id / gen_ai.response.model).
	ResponseID      string
	ResponseModelID string
	Response        interface{}

	// AIVersion is the version of the Go AI SDK that emitted the event.
	AIVersion string
}

type extractStartHandler interface {
	OnExtractStart(context.Context, ExtractStartEvent) context.Context
}

type extractEndHandler interface {
	OnExtractEnd(context.Context, ExtractEndEvent)
}

type extractionModelCallStartHandler interface {
	OnExtractionModelCallStart(context.Context, ExtractionModelCallStartEvent)
}

type extractionModelCallEndHandler interface {
	OnExtractionModelCallEnd(context.Context, ExtractionModelCallEndEvent)
}

// toTelemetryExtractStart is TS toTelemetryEvent: no headers, no provider
// options, file locations in the state redacted.
func toTelemetryExtractStart(e ExtractStartEvent) ExtractStartEvent {
	e.Headers = nil
	e.ProviderOptions = nil
	e.State = redactFileLocations(e.State)
	return e
}

// FireOnExtractStart publishes and fans out an extract-operation start
// event to integrations implementing OnExtractStart. Every integration gets
// the same base ctx (see FireOnDecideStart).
func FireOnExtractStart(ctx context.Context, e ExtractStartEvent) context.Context {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return ctx
	}
	e = FilterEvent(toTelemetryExtractStart(e), e.Settings)
	PublishDiagnostic(ctx, DiagnosticEventOnExtractStart, e)
	result := ctx
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(extractStartHandler); ok {
			callIntegration(func() { result = handler.OnExtractStart(ctx, e) })
		}
	}
	return result
}

// FireOnExtractEnd publishes and fans out an extract-operation end event.
func FireOnExtractEnd(ctx context.Context, e ExtractEndEvent) {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return
	}
	e.ExtractStartEvent = toTelemetryExtractStart(e.ExtractStartEvent)
	e = FilterEvent(e, e.Settings)
	PublishDiagnostic(ctx, DiagnosticEventOnExtractEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(extractEndHandler); ok {
			callIntegration(func() { handler.OnExtractEnd(ctx, e) })
		}
	}
}

// FireOnExtractionModelCallStart publishes and fans out an
// extraction-model-call start event. File locations in the state are
// redacted.
func FireOnExtractionModelCallStart(ctx context.Context, e ExtractionModelCallStartEvent) {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return
	}
	e.State = redactFileLocations(e.State)
	e = FilterEvent(e, e.Settings)
	PublishDiagnostic(ctx, DiagnosticEventOnExtractionModelCallStart, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(extractionModelCallStartHandler); ok {
			callIntegration(func() { handler.OnExtractionModelCallStart(ctx, e) })
		}
	}
}

// FireOnExtractionModelCallEnd publishes and fans out an
// extraction-model-call end event. File locations in the state are redacted.
func FireOnExtractionModelCallEnd(ctx context.Context, e ExtractionModelCallEndEvent) {
	e.AIVersion = sdkversion.Version
	if telemetryDisabled(e.Settings) {
		return
	}
	e.State = redactFileLocations(e.State)
	e = FilterEvent(e, e.Settings)
	PublishDiagnostic(ctx, DiagnosticEventOnExtractionModelCallEnd, e)
	for _, integration := range snapshotFor(e.Settings) {
		if handler, ok := integration.(extractionModelCallEndHandler); ok {
			callIntegration(func() { handler.OnExtractionModelCallEnd(ctx, e) })
		}
	}
}

// redactFileLocations is TS redactFileLocations (with 797dc0254e): in a state
// that is a list of parts, every text and file part loses its provider options and
// every file part whose data is a location (a URL that is not a
// data: URL, or a provider reference) gets its data replaced by
// "[redacted]". Any other state is returned as is. state itself is never
// modified.
//
// The parts are read by shape (a struct with a string Type and a
// *types.FileContent File, as provider.DecisionStatePart), because this
// package cannot import pkg/provider.
func redactFileLocations(state interface{}) interface{} {
	v := reflect.ValueOf(state)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.IsNil() {
		return state
	}
	elem := v.Type().Elem()
	if elem.Kind() != reflect.Struct {
		return state
	}
	typeField, ok := elem.FieldByName("Type")
	if !ok || typeField.Type.Kind() != reflect.String {
		return state
	}
	fileField, ok := elem.FieldByName("File")
	if !ok || fileField.Type != reflect.TypeOf((*types.FileContent)(nil)) {
		return state
	}
	var out reflect.Value
	_, hasPartOptions := elem.FieldByName("ProviderOptions")
	for i := 0; i < v.Len(); i++ {
		part := v.Index(i)
		if part.FieldByName("Type").String() == "text" {
			// TS 797dc0254e: a text part's provider options never reach
			// telemetry either.
			if po := part.FieldByName("ProviderOptions"); hasPartOptions && po.Kind() == reflect.Map && !po.IsNil() {
				if !out.IsValid() {
					out = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
					reflect.Copy(out, v)
				}
				cp := reflect.New(elem).Elem()
				cp.Set(part)
				cp.FieldByName("ProviderOptions").Set(reflect.Zero(po.Type()))
				out.Index(i).Set(cp)
			}
			continue
		}
		if part.FieldByName("Type").String() != "file" {
			continue
		}
		file, _ := part.FieldByName("File").Interface().(*types.FileContent)
		location := file != nil && isFileLocation(file)
		if file == nil || (!location && file.ProviderOptions == nil) {
			continue
		}
		if !out.IsValid() {
			out = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
			reflect.Copy(out, v)
		}
		// TS 797dc0254e: part-level provider options never reach telemetry.
		redacted := *file
		redacted.ProviderOptions = nil
		if location {
			redacted.URL = ""
			redacted.Data = nil
			redacted.Text = ""
			redacted.FileData = types.FileData{Type: types.FileDataTypeData, DataString: redactedToolValue, MediaType: file.FileData.MediaType}
		}
		cp := reflect.New(elem).Elem()
		cp.Set(part)
		cp.FieldByName("File").Set(reflect.ValueOf(&redacted))
		out.Index(i).Set(cp)
	}
	if !out.IsValid() {
		return state
	}
	return out.Interface()
}

// isFileLocation is TS isFileLocation: a URL, a provider reference, or a
// string that parses as a URL with a scheme other than data:.
func isFileLocation(f *types.FileContent) bool {
	switch f.FileData.Type {
	case types.FileDataTypeURL, types.FileDataTypeReference:
		return true
	case types.FileDataTypeData:
		if f.FileData.DataString != "" {
			return isNonDataURLString(f.FileData.DataString)
		}
		return false
	case types.FileDataTypeText:
		return false
	}
	return f.URL != "" && isNonDataURLString(f.URL)
}

func isNonDataURLString(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Scheme != "data"
}

// ---------------------------------------------------------------------------
// OpenTelemetry (GenAI semconv): extract spans
// ---------------------------------------------------------------------------

func (i OpenTelemetry) extractionInputAttrs(settings *Settings, state, fields interface{}, referenceDate string) []attribute.KeyValue {
	if !i.opts.ExperimentalExtraction || !settings.ShouldRecordInputs() {
		return nil
	}
	var attrs []attribute.KeyValue
	if attr, ok := jsonAttr("ai.extraction.state", state); ok {
		attrs = append(attrs, attr)
	}
	if attr, ok := jsonAttr("ai.extraction.fields", fields); ok {
		attrs = append(attrs, attr)
	}
	if referenceDate != "" {
		attrs = append(attrs, attribute.String("ai.extraction.referenceDate", referenceDate))
	}
	return attrs
}

// OnExtractStart creates the root "extract {modelId}" span, mirroring TS
// experimental_onExtractStart.
func (i OpenTelemetry) OnExtractStart(ctx context.Context, e ExtractStartEvent) context.Context {
	if !Enabled(e.Settings) {
		return ctx
	}
	tracer := i.tracerFor(e.Settings)
	ctx, span := tracer.Start(ctx, "extract "+e.ModelID, trace.WithSpanKind(trace.SpanKindClient))
	if attrs := i.customAttrs(ctx, e.Settings, EnrichSpanOptions{
		SpanType:       SpanTypeOperation,
		OperationType:  e.OperationID,
		CallID:         e.CallID,
		RuntimeContext: e.RuntimeContext,
	}); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "extract"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
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
	span.SetAttributes(i.extractionInputAttrs(e.Settings, e.State, e.Fields, e.ReferenceDate)...)
	if e.CallID != "" {
		st := genAIState(i.stateKey(e.CallID))
		st.mu.Lock()
		st.rootSpan = span
		st.baseSupp = baseSupp
		st.tracer = tracer
		st.operationType = e.OperationID
		st.runtimeContext = e.RuntimeContext
		st.mu.Unlock()
	}
	return ctx
}

// OnExtractEnd records ai.extraction.answers on the root span and ends it,
// mirroring TS experimental_onExtractEnd.
func (i OpenTelemetry) OnExtractEnd(ctx context.Context, e ExtractEndEvent) {
	defer genAIDeleteState(i.stateKey(e.CallID))
	span := genAIRootSpanFor(i.stateKey(e.CallID), ctx)
	if !span.IsRecording() {
		return
	}
	if i.opts.ExperimentalExtraction && e.Settings.ShouldRecordOutputs() {
		if attr, ok := jsonAttr("ai.extraction.answers", e.Answers); ok {
			span.SetAttributes(attr)
		}
	}
	span.End()
	genAIDeleteState(i.stateKey(e.CallID))
}

// OnExtractionModelCallStart creates the nested "extract {modelId}" span for
// the model call, mirroring TS experimental_onExtractionModelCallStart.
func (i OpenTelemetry) OnExtractionModelCallStart(ctx context.Context, e ExtractionModelCallStartEvent) {
	parent := genAIRootSpanFor(i.stateKey(e.CallID), ctx)
	if !parent.IsRecording() {
		return
	}
	tracer := genAIChildTracer(i.stateKey(e.CallID), parent)
	_, span := tracer.Start(trace.ContextWithSpan(ctx, parent), "extract "+e.ModelID, trace.WithSpanKind(trace.SpanKindClient))
	enrichOpts := i.childEnrichOptions(SpanTypeExtraction, e.CallID)
	enrichOpts.OperationType = e.OperationID
	if attrs := i.customAttrs(ctx, e.Settings, enrichOpts); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "extract"),
		attribute.String("gen_ai.provider.name", mapProviderName(e.ModelProvider)),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	span.SetAttributes(genAIBaseSupplemental(i.stateKey(e.CallID))...)
	span.SetAttributes(i.extractionInputAttrs(e.Settings, e.State, e.Fields, e.ReferenceDate)...)
	if e.CallID != "" {
		genAICallSpans.Store(genAISpanKey("extraction", i.stateKey(e.CallID)), otelSpanEntry{span: span})
	}
}

// OnExtractionModelCallEnd records the response, usage and answers on the
// nested span and ends it, mirroring TS experimental_onExtractionModelCallEnd.
func (i OpenTelemetry) OnExtractionModelCallEnd(_ context.Context, e ExtractionModelCallEndEvent) {
	value, ok := genAICallSpans.LoadAndDelete(genAISpanKey("extraction", i.stateKey(e.CallID)))
	if !ok {
		return
	}
	entry, ok := value.(otelSpanEntry)
	if !ok || !entry.span.IsRecording() {
		return
	}
	if e.ResponseID != "" {
		entry.span.SetAttributes(attribute.String("gen_ai.response.id", e.ResponseID))
	}
	if e.ResponseModelID != "" {
		entry.span.SetAttributes(attribute.String("gen_ai.response.model", e.ResponseModelID))
	}
	if e.Usage.InputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		entry.span.SetAttributes(attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}
	if i.opts.ExperimentalExtraction && e.Settings.ShouldRecordOutputs() {
		if attr, ok := jsonAttr("ai.extraction.answers", e.Answers); ok {
			entry.span.SetAttributes(attr)
		}
	}
	entry.span.End()
}

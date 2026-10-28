// Package telemetry provides OpenTelemetry integration for the AI SDK.
// It allows tracking and monitoring of AI operations including generation,
// embedding, and streaming with customizable spans and attributes.
package telemetry

import (
	"context"
)

// SpanType identifies the OTel span being created for custom enrichment.
type SpanType string

const (
	SpanTypeOperation     SpanType = "operation"
	SpanTypeStep          SpanType = "step"
	SpanTypeLanguageModel SpanType = "languageModel"
	SpanTypeTool          SpanType = "tool"
	SpanTypeEmbedding     SpanType = "embedding"
	SpanTypeReranking     SpanType = "reranking"
	SpanTypeDecision      SpanType = "experimental_decision"
)

// EnrichSpanOptions describes a span that is about to be created. The
// OpenTelemetry (GenAI) integration passes what TS enrichSpan receives: the
// call's operation (for example "ai.streamText"; a decision model call has its
// own operation id), the call ID, and the call's runtime context (none for
// object and audio calls).
type EnrichSpanOptions struct {
	SpanType       SpanType
	OperationType  string
	CallID         string
	RuntimeContext map[string]interface{}
}

// EnrichSpanFunc returns custom attributes for a span when it is created.
// SDK-managed attributes win if a custom key overlaps with an SDK key.
type EnrichSpanFunc func(context.Context, EnrichSpanOptions) map[string]interface{}

// Options configures telemetry for AI operations.
// Telemetry is active by default when integrations are registered. Set
// IsEnabled to Bool(false) on per-call options to opt out.
type Options struct {
	// IsEnabled controls whether telemetry is active.
	// nil means enabled, matching the TypeScript SDK's optional isEnabled field.
	IsEnabled *bool

	// RecordInputs controls whether input data (prompts, messages, tool
	// arguments, embedding values, rerank documents) is recorded in spans.
	// nil means record, as an omitted recordInputs in TS; set it to
	// Bool(false) to keep inputs out of spans, for example to avoid
	// recording sensitive information, to reduce data transfers, or to
	// increase performance. Read it with ShouldRecordInputs.
	RecordInputs *bool

	// RecordOutputs controls whether output data (generated text, objects,
	// tool results, embeddings) is recorded in spans. nil means record, as an
	// omitted recordOutputs in TS; set it to Bool(false) to keep outputs out
	// of spans. Read it with ShouldRecordOutputs.
	RecordOutputs *bool

	// FunctionID is an identifier for grouping telemetry data by function or operation.
	FunctionID string

	// EnrichSpan adds custom attributes to OTel spans as they are created.
	// SDK-managed attributes override custom attributes on key collisions.
	EnrichSpan EnrichSpanFunc

	// IncludeRuntimeContext lists top-level runtime context keys that should be
	// included in telemetry. Context is excluded by default.
	IncludeRuntimeContext map[string]bool

	// IncludeToolsContext lists top-level tool context keys that should be
	// included in telemetry per tool. Context is excluded by default.
	IncludeToolsContext map[string]map[string]bool

	// Integrations are per-call telemetry integrations. When non-nil, they
	// replace the globally registered integrations for this call, as TS
	// `integrations` does: an empty, non-nil slice means no integrations
	// (nothing is emitted for the call). nil uses the global integrations.
	// (v0.5.x treated an empty slice like nil.)
	Integrations []TelemetryIntegration
}

// Settings configures telemetry for AI operations.
//
// Deprecated: use Options.
type Settings = Options

// DefaultSettings returns Settings with sensible defaults.
func DefaultSettings() *Settings {
	return &Settings{
		IsEnabled:     Bool(true),
		RecordInputs:  Bool(true),
		RecordOutputs: Bool(true),
	}
}

// Bool returns a bool pointer for optional telemetry fields.
func Bool(v bool) *bool {
	return &v
}

// ShouldRecordInputs reports whether inputs are recorded: true unless
// RecordInputs is set to false. A nil *Options records inputs.
func (s *Options) ShouldRecordInputs() bool {
	return s == nil || s.RecordInputs == nil || *s.RecordInputs
}

// ShouldRecordOutputs reports whether outputs are recorded: true unless
// RecordOutputs is set to false. A nil *Options records outputs.
func (s *Options) ShouldRecordOutputs() bool {
	return s == nil || s.RecordOutputs == nil || *s.RecordOutputs
}

// Enabled reports whether telemetry should run for settings.
func Enabled(settings *Settings) bool {
	return settings == nil || settings.IsEnabled == nil || *settings.IsEnabled
}

// WithEnabled returns a copy of Settings with IsEnabled set to the given value.
func (s *Settings) WithEnabled(enabled bool) *Settings {
	copy := *s
	copy.IsEnabled = Bool(enabled)
	return &copy
}

// WithRecordInputs returns a copy of Settings with RecordInputs set to the given value.
func (s *Settings) WithRecordInputs(record bool) *Settings {
	copy := *s
	copy.RecordInputs = Bool(record)
	return &copy
}

// WithRecordOutputs returns a copy of Settings with RecordOutputs set to the given value.
func (s *Settings) WithRecordOutputs(record bool) *Settings {
	copy := *s
	copy.RecordOutputs = Bool(record)
	return &copy
}

// WithFunctionID returns a copy of Settings with FunctionID set to the given value.
func (s *Settings) WithFunctionID(id string) *Settings {
	copy := *s
	copy.FunctionID = id
	return &copy
}

// WithTracer no longer exists: Options.Tracer was removed (9b47dea) because a
// single process-wide field could not express "each registered integration
// gets its own tracer." Configure a tracer on the integration itself instead:
// telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: t})
// or telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: t}).

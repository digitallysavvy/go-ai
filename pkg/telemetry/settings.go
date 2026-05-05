// Package telemetry provides OpenTelemetry integration for the AI SDK.
// It allows tracking and monitoring of AI operations including generation,
// embedding, and streaming with customizable spans and attributes.
package telemetry

import (
	"go.opentelemetry.io/otel/trace"
)

// Options configures telemetry for AI operations.
// Telemetry is active by default when integrations are registered. Set
// IsEnabled to Bool(false) on per-call options to opt out.
type Options struct {
	// IsEnabled controls whether telemetry is active.
	// nil means enabled, matching the TypeScript SDK's optional isEnabled field.
	IsEnabled *bool

	// RecordInputs controls whether input data is recorded in spans. Defaults to true when telemetry is enabled.
	// You might want to disable input recording to avoid recording sensitive
	// information, to reduce data transfers, or to increase performance.
	RecordInputs bool

	// RecordOutputs controls whether output data is recorded in spans. Defaults to true when telemetry is enabled.
	// You might want to disable output recording to avoid recording sensitive
	// information, to reduce data transfers, or to increase performance.
	RecordOutputs bool

	// FunctionID is an identifier for grouping telemetry data by function or operation.
	FunctionID string

	// Tracer is a custom OpenTelemetry tracer. If nil, the global tracer will be used.
	Tracer trace.Tracer

	// IncludeRuntimeContext lists top-level runtime context keys that should be
	// included in telemetry. Context is excluded by default.
	IncludeRuntimeContext map[string]bool

	// IncludeToolsContext lists top-level tool context keys that should be
	// included in telemetry per tool. Context is excluded by default.
	IncludeToolsContext map[string]map[string]bool

	// Integrations are per-call telemetry integrations. When non-empty, they
	// replace globally registered integrations for this call.
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
		RecordInputs:  true,
		RecordOutputs: true,
	}
}

// Bool returns a bool pointer for optional telemetry fields.
func Bool(v bool) *bool {
	return &v
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
	copy.RecordInputs = record
	return &copy
}

// WithRecordOutputs returns a copy of Settings with RecordOutputs set to the given value.
func (s *Settings) WithRecordOutputs(record bool) *Settings {
	copy := *s
	copy.RecordOutputs = record
	return &copy
}

// WithFunctionID returns a copy of Settings with FunctionID set to the given value.
func (s *Settings) WithFunctionID(id string) *Settings {
	copy := *s
	copy.FunctionID = id
	return &copy
}

// WithTracer returns a copy of Settings with Tracer set to the given value.
func (s *Settings) WithTracer(tracer trace.Tracer) *Settings {
	copy := *s
	copy.Tracer = tracer
	return &copy
}

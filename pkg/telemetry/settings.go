// Package telemetry provides OpenTelemetry integration for the AI SDK.
// It allows tracking and monitoring of AI operations including generation,
// embedding, and streaming with customizable spans and attributes.
package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Settings configures telemetry for AI operations.
// Telemetry is disabled by default and must be explicitly enabled.
type Settings struct {
	// IsEnabled controls whether telemetry is active. Defaults to false.
	IsEnabled bool

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

	// Metadata contains additional key-value pairs to include in telemetry spans.
	Metadata map[string]attribute.Value

	// Tracer is a custom OpenTelemetry tracer. If nil, the global tracer will be used.
	Tracer trace.Tracer
}

// DefaultSettings returns Settings with sensible defaults.
func DefaultSettings() *Settings {
	return &Settings{
		IsEnabled:     false,
		RecordInputs:  true,
		RecordOutputs: true,
		Metadata:      make(map[string]attribute.Value),
	}
}

// WithEnabled returns a copy of Settings with IsEnabled set to the given value.
func (s *Settings) WithEnabled(enabled bool) *Settings {
	copy := *s
	copy.IsEnabled = enabled
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

// WithMetadata returns a copy of Settings with the given metadata merged in.
func (s *Settings) WithMetadata(metadata map[string]attribute.Value) *Settings {
	copy := *s
	copy.Metadata = make(map[string]attribute.Value)
	for k, v := range s.Metadata {
		copy.Metadata[k] = v
	}
	for k, v := range metadata {
		copy.Metadata[k] = v
	}
	return &copy
}

// WithTracer returns a copy of Settings with Tracer set to the given value.
func (s *Settings) WithTracer(tracer trace.Tracer) *Settings {
	copy := *s
	copy.Tracer = tracer
	return &copy
}

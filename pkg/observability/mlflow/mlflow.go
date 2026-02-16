// Package mlflow provides MLflow observability integration for the Go AI SDK.
// It enables automatic tracking of AI operations including prompts, responses,
// token usage, latencies, and costs to MLflow Tracking Server.
//
// MLflow Tracing provides experiment tracking and observability for AI applications
// via OpenTelemetry. When enabled, MLflow records:
//   - Prompts/messages and generated responses
//   - Latencies and call hierarchy
//   - Token usage (when the provider returns it)
//   - Exceptions and errors
//
// Example usage:
//
//	tracker := mlflow.New(mlflow.Config{
//	    TrackingURI:    "http://localhost:5000",
//	    ExperimentName: "my-ai-app",
//	})
//	defer tracker.Shutdown(context.Background())
//
//	// Use with middleware or direct telemetry settings
//	tracer := tracker.Tracer()
package mlflow

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config holds configuration for MLflow observability
type Config struct {
	// TrackingURI is the MLflow tracking server endpoint
	// Example: "http://localhost:5000" or "https://mlflow.example.com"
	TrackingURI string

	// ExperimentName is the name of the MLflow experiment to log to
	// If not provided, uses "default"
	ExperimentName string

	// ExperimentID is the MLflow experiment ID (optional)
	// Takes precedence over ExperimentName if both are provided
	ExperimentID string

	// ServiceName is the name of the service for OpenTelemetry
	// If not provided, uses "go-ai-sdk"
	ServiceName string

	// Insecure controls whether to use insecure HTTP connection
	// Set to true for local development without TLS
	// Default: false (uses HTTPS)
	Insecure bool

	// Headers contains additional headers to send with trace exports
	// Example: map[string]string{"Authorization": "Bearer token"}
	Headers map[string]string
}

// Tracker manages MLflow observability integration
type Tracker struct {
	config         Config
	tracerProvider *sdktrace.TracerProvider
	exporter       *otlptrace.Exporter
}

// New creates a new MLflow tracker with the provided configuration
func New(cfg Config) (*Tracker, error) {
	if cfg.TrackingURI == "" {
		return nil, fmt.Errorf("mlflow: TrackingURI is required")
	}

	// Parse and validate tracking URI
	parsedURI, err := url.Parse(cfg.TrackingURI)
	if err != nil {
		return nil, fmt.Errorf("mlflow: invalid TrackingURI: %w", err)
	}

	// Set defaults
	if cfg.ServiceName == "" {
		cfg.ServiceName = "go-ai-sdk"
	}
	if cfg.ExperimentName == "" && cfg.ExperimentID == "" {
		cfg.ExperimentName = "default"
	}

	// Build OTLP trace endpoint
	// MLflow expects traces at /v1/traces endpoint
	endpoint := parsedURI.Host
	if parsedURI.Port() != "" {
		endpoint = parsedURI.Hostname() + ":" + parsedURI.Port()
	}

	// Build headers including experiment ID/name
	headers := make(map[string]string)
	if cfg.ExperimentID != "" {
		headers["x-mlflow-experiment-id"] = cfg.ExperimentID
	} else if cfg.ExperimentName != "" {
		headers["x-mlflow-experiment-name"] = cfg.ExperimentName
	}
	// Merge additional headers
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	// Create OTLP HTTP exporter configured for MLflow
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithURLPath("/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("mlflow: failed to create OTLP exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("mlflow: failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set as global tracer provider
	otel.SetTracerProvider(tp)

	return &Tracker{
		config:         cfg,
		tracerProvider: tp,
		exporter:       exporter,
	}, nil
}

// Tracer returns an OpenTelemetry tracer that can be used with AI SDK telemetry settings
func (t *Tracker) Tracer() trace.Tracer {
	return t.tracerProvider.Tracer("ai-sdk")
}

// Shutdown gracefully shuts down the tracker, flushing any pending spans
func (t *Tracker) Shutdown(ctx context.Context) error {
	if t.tracerProvider != nil {
		if err := t.tracerProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("mlflow: failed to shutdown tracer provider: %w", err)
		}
	}
	return nil
}

// ForceFlush forces any pending spans to be exported immediately
func (t *Tracker) ForceFlush(ctx context.Context) error {
	if t.tracerProvider != nil {
		if err := t.tracerProvider.ForceFlush(ctx); err != nil {
			return fmt.Errorf("mlflow: failed to flush spans: %w", err)
		}
	}
	return nil
}

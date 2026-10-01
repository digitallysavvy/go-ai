package ai

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// H4 item 2: "span leak on provider error". When the provider call itself
// fails (doGenerate/doStream returning an error), the step span (and, for
// the GenAI integration, the nested "chat" span) used to never get ended,
// because the deferred FireOnError/FireOnAbort call at the top of
// generateText/streamText/generateObject/streamObject only ever sees the
// outer, unnested ctx — the step/model-call spans live in a ctx returned by
// FireOnStepStart/FireOnLanguageModelCallStart, local to that call, which
// never gets threaded back out. These tests assert that every span started
// for a failed call is also ended: rec.Started() and rec.Ended() must have
// the same length (a leaked span is Started but never Ended).
//
// Each case below runs once per integration (LegacyOpenTelemetry, then
// OpenTelemetry/GenAI) rather than with both registered simultaneously:
// registering both at once exposes a separate, pre-existing issue (root-span
// attribute/End() collisions via trace.SpanFromContext when two integrations
// chain root spans in FireOnStart) that predates this fix and is out of
// scope for H4 item 2, which is specifically about the step/model-call span
// leak — see the H4 final report for details.

func legacyOnlySettings(rec *tracetest.SpanRecorder) (*telemetry.Options, func()) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	tracer := tp.Tracer("span-leak-legacy-test")
	settings := &telemetry.Options{
		IsEnabled:    telemetry.Bool(true),
		Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
	}
	return settings, func() { _ = tp.Shutdown(context.Background()) }
}

func genAIOnlySettings(rec *tracetest.SpanRecorder) (*telemetry.Options, func()) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	tracer := tp.Tracer("span-leak-genai-test")
	settings := &telemetry.Options{
		IsEnabled:    telemetry.Bool(true),
		Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
	}
	return settings, func() { _ = tp.Shutdown(context.Background()) }
}

// assertNoOpenSpans fails the test if any span recorded by rec was started
// but never ended.
func assertNoOpenSpans(t *testing.T, rec *tracetest.SpanRecorder) {
	t.Helper()
	started := rec.Started()
	ended := rec.Ended()
	if len(started) == 0 {
		t.Fatal("expected at least one span to have been started")
	}
	if len(started) == len(ended) {
		return
	}
	endedNames := make(map[string]int)
	for _, s := range ended {
		endedNames[s.Name()]++
	}
	var leaked []string
	for _, s := range started {
		name := s.Name()
		if endedNames[name] > 0 {
			endedNames[name]--
			continue
		}
		leaked = append(leaked, name)
	}
	t.Fatalf("started spans = %d, ended spans = %d; leaked (started but never ended): %v", len(started), len(ended), leaked)
}

func TestGenerateText_ProviderErrorClosesAllSpans(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings func(*tracetest.SpanRecorder) (*telemetry.Options, func())
	}{
		{"legacy", legacyOnlySettings},
		{"genai", genAIOnlySettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			settings, cleanup := tc.settings(rec)
			defer cleanup()

			wantErr := errors.New("provider boom")
			model := &testutil.MockLanguageModel{
				ProviderName: "test-provider",
				ModelName:    "test-model",
				DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
					return nil, wantErr
				},
			}

			_, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:     model,
				Prompt:    "hello",
				Telemetry: settings,
			})
			if err == nil {
				t.Fatal("expected GenerateText to return an error")
			}
			assertNoOpenSpans(t, rec)
		})
	}
}

func TestStreamText_ProviderErrorClosesAllSpans(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings func(*tracetest.SpanRecorder) (*telemetry.Options, func())
	}{
		{"legacy", legacyOnlySettings},
		{"genai", genAIOnlySettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			settings, cleanup := tc.settings(rec)
			defer cleanup()

			wantErr := errors.New("provider boom")
			model := &testutil.MockLanguageModel{
				ProviderName: "test-provider",
				ModelName:    "test-model",
				DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
					return nil, wantErr
				},
			}

			stream, err := StreamText(context.Background(), StreamTextOptions{
				Model:     model,
				Prompt:    "hello",
				Telemetry: settings,
			})
			if err != nil {
				t.Fatalf("StreamText constructor error: %v", err)
			}
			if _, readErr := stream.ReadAll(); readErr == nil {
				t.Fatal("expected ReadAll to surface the provider error")
			}
			assertNoOpenSpans(t, rec)
		})
	}
}

func TestGenerateObject_ProviderErrorClosesAllSpans(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings func(*tracetest.SpanRecorder) (*telemetry.Options, func())
	}{
		{"legacy", legacyOnlySettings},
		{"genai", genAIOnlySettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			settings, cleanup := tc.settings(rec)
			defer cleanup()

			wantErr := errors.New("provider boom")
			model := &testutil.MockLanguageModel{
				ProviderName:      "test-provider",
				ModelName:         "test-model",
				StructuredSupport: true,
				DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
					return nil, wantErr
				},
			}
			testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
			})

			_, err := GenerateObject(context.Background(), GenerateObjectOptions{
				Model:                 model,
				Prompt:                "Generate a person",
				Schema:                testSchema,
				ExperimentalTelemetry: settings,
			})
			if err == nil {
				t.Fatal("expected GenerateObject to return an error")
			}
			assertNoOpenSpans(t, rec)
		})
	}
}

func TestStreamObject_ProviderErrorClosesAllSpans(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings func(*tracetest.SpanRecorder) (*telemetry.Options, func())
	}{
		{"legacy", legacyOnlySettings},
		{"genai", genAIOnlySettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			settings, cleanup := tc.settings(rec)
			defer cleanup()

			wantErr := errors.New("provider boom")
			model := &testutil.MockLanguageModel{
				ProviderName:      "test-provider",
				ModelName:         "test-model",
				StructuredSupport: true,
				DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
					return nil, wantErr
				},
			}
			testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
			})

			_, err := StreamObject(context.Background(), StreamObjectOptions{
				Model:                 model,
				Prompt:                "Generate a person",
				Schema:                testSchema,
				ExperimentalTelemetry: settings,
			})
			if err == nil {
				t.Fatal("expected StreamObject to return an error")
			}
			assertNoOpenSpans(t, rec)
		})
	}
}

package ai

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generate-speech.ts: the end event carries `providerMetadata:
// result.providerMetadata`, undefined when the speech model reported none,
// so neither OTel integration records ai.response.providerMetadata
// (legacy-open-telemetry.ts / open-telemetry.ts onAudioOperationEnd:
// `event.providerMetadata == null ? undefined : JSON.stringify(...)`). The
// result's providerMetadata is still `{}`.
func TestR93carrySpeechEndEventProviderMetadataAsReported(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata map[string]interface{}
		wantAttr string
	}{
		{name: "none", metadata: nil, wantAttr: ""},
		{name: "reported", metadata: map[string]interface{}{"p": map[string]interface{}{"k": "v"}}, wantAttr: `{"p":{"k":"v"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &testutil.MockSpeechModel{
				DoGenerateFunc: func(context.Context, *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
					return &types.SpeechResult{Audio: []byte("RIFF....WAVEfmt "), ProviderMetadata: tc.metadata}, nil
				},
			}
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			defer func() { _ = tp.Shutdown(context.Background()) }()
			events := &r92carrySpeechRecorder{}
			legacy := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("ai")})
			genAI := telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("gen_ai"), ProviderMetadata: true})
			res, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
				Model: model, Text: "Hello",
				Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{events, legacy, genAI}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.ProviderMetadata == nil {
				t.Fatal("result providerMetadata is nil, want {}")
			}
			if len(events.end) != 1 {
				t.Fatalf("end events = %d", len(events.end))
			}
			if got := events.end[0].ProviderMetadata; (got == nil) != (tc.metadata == nil) {
				t.Fatalf("end event providerMetadata = %#v, want %#v", got, tc.metadata)
			}
			spans := rec.Ended()
			if len(spans) != 2 {
				t.Fatalf("spans = %d, want 2", len(spans))
			}
			for _, s := range spans {
				got := ""
				for _, kv := range s.Attributes() {
					if kv.Key == attribute.Key("ai.response.providerMetadata") {
						got = kv.Value.AsString()
					}
				}
				if got != tc.wantAttr {
					t.Errorf("span %q ai.response.providerMetadata = %q, want %q", s.Name(), got, tc.wantAttr)
				}
			}
		})
	}
}

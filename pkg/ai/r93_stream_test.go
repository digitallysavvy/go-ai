package ai

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

var r93streamNumberArray = ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "number"})})

// r93streamExternal runs parts through NewStreamTextResultFromParts as a
// harness turn with output.
func r93streamExternal(parts []provider.StreamChunk, output interface{}, closesTurn bool) *StreamTextResult {
	return NewStreamTextResultFromParts(context.Background(), &r92streamParts{parts: parts}, ExternalStreamOptions{Output: output, TerminalFinishClosesTurn: closesTurn})
}

// TS harness-stream-text-result.ts elementStream pipes the turn's enriched
// parts (its createOutputTransformStream: the turn's first text block, not
// reset at step boundaries) through createElementStreamTransform, so the
// element stream follows the partial outputs the turn published.
func TestR93streamExternalElementStreamTurnFirstBlock(t *testing.T) {
	finishStep := provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	// One step: every element is streamed.
	r := r93streamExternal([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "s"},
		{Type: provider.ChunkTypeText, ID: "s", Text: `{"elements":[1,`},
		{Type: provider.ChunkTypeText, ID: "s", Text: `2]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "s"},
		finishStep, finish,
	}, r93streamNumberArray, true)
	if got := r92streamJSON(r92streamElements(t, r, r93streamNumberArray)); got != `[1,2]` {
		t.Fatalf("one step: elements = %s", got)
	}
	// A tool step's text block comes first: the final step's JSON block is
	// not the turn's first text block, so TS publishes no partial and the
	// element stream is empty (as PartialOutput is).
	r = r93streamExternal([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "a"},
		{Type: provider.ChunkTypeText, ID: "a", Text: "Let me check."},
		{Type: provider.ChunkTypeTextEnd, ID: "a"},
		finishStep,
		{Type: provider.ChunkTypeTextStart, ID: "b"},
		{Type: provider.ChunkTypeText, ID: "b", Text: `{"elements":[1,2]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "b"},
		finishStep, finish,
	}, r93streamNumberArray, true)
	if got := r92streamElements(t, r, r93streamNumberArray); len(got) != 0 {
		t.Fatalf("two steps: elements = %v, want none", got)
	}
	if r.PartialOutput() != nil {
		t.Fatalf("two steps: partial = %#v, want none", r.PartialOutput())
	}
	// The JSON block of the first step keeps feeding the partial in a later
	// step (TS does not reset at start-step).
	r = r93streamExternal([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "s"},
		{Type: provider.ChunkTypeText, ID: "s", Text: `{"elements":[1,`},
		finishStep,
		{Type: provider.ChunkTypeText, ID: "s", Text: `2,3]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "s"},
		finishStep, finish,
	}, r93streamNumberArray, true)
	if got := r92streamJSON(r92streamElements(t, r, r93streamNumberArray)); got != `[1,2,3]` {
		t.Fatalf("block across steps: elements = %s", got)
	}
}

// TS elementStream is its own tee of the result's stream: reading the full
// stream first does not take its parts away from a later element stream.
func TestR93streamElementStreamOwnCursor(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(`{"elements":[1,2,3]}`)), Prompt: "numbers"})
	if err != nil {
		t.Fatal(err)
	}
	full := r91streamDrain(r.Stream())
	if len(full) == 0 {
		t.Fatal("empty full stream")
	}
	var got []float64
	for e := range ElementStream[float64](r, ElementStreamOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "number"})}) {
		got = append(got, e.Element)
	}
	if r92streamJSON(got) != `[1,2,3]` {
		t.Fatalf("elements after the full stream was read = %v", got)
	}
}

// TS stream-transcribe.ts: the end event carries `providerMetadata:
// value.providerMetadata`, undefined when the model reported none, so the
// OTel integrations record no ai.response.providerMetadata; only the result
// resolves `value.providerMetadata ?? {}` (cr-r93-carry-to-stream).
func TestR93streamTranscribeEndEventProviderMetadataAsReported(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata map[string]interface{}
		wantAttr string
	}{
		{name: "none", metadata: nil, wantAttr: ""},
		{name: "reported", metadata: map[string]interface{}{"p": map[string]interface{}{"k": "v"}}, wantAttr: `{"p":{"k":"v"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &mockTranscriptionStreamerModel{
				doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
					for {
						if _, err := opts.Audio.Next(ctx); err != nil {
							break
						}
					}
					return sliceStreamResult(
						provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
						provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "hello", ProviderMetadata: tc.metadata},
					), nil
				},
			}
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			defer func() { _ = tp.Shutdown(context.Background()) }()
			legacy := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("ai")})
			genAI := telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("gen_ai"), ProviderMetadata: true})
			result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
				Model:            model,
				Audio:            newMockChanAudioStream([]byte("abc")),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
				Telemetry:        &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{legacy, genAI}},
			})
			if err != nil {
				t.Fatal(err)
			}
			md, err := result.ProviderMetadata()
			if err != nil || md == nil {
				t.Fatalf("ProviderMetadata() = %#v, %v; want non-nil", md, err)
			}
			spans := rec.Ended()
			if len(spans) == 0 {
				t.Fatal("no spans")
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

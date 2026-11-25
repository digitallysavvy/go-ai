package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type startInputsRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts []telemetry.TelemetryStartEvent
}

func (r *startInputsRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	r.starts = append(r.starts, e)
	r.mu.Unlock()
	return ctx
}

// Without per-call telemetry options a registered integration records inputs
// (TS recordInputs defaults to true, and Go integrations treat a nil *Options
// the same way), so the start event carries the system prompt and messages.
func TestTextStartEventCarriesInputsWithoutTelemetryOptions(t *testing.T) {
	rec := &startInputsRecorder{}
	telemetry.RegisterTelemetryIntegration(rec)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, System: "be brief", Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	stream, err := StreamText(context.Background(), StreamTextOptions{Model: model, System: "be brief", Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatal(err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.starts) != 2 {
		t.Fatalf("got %d start events, want 2", len(rec.starts))
	}
	for _, e := range rec.starts {
		if e.System != "be brief" || len(e.Messages) != 1 {
			t.Fatalf("%s start event: system=%q messages=%d, want the inputs", e.OperationType, e.System, len(e.Messages))
		}
	}
}

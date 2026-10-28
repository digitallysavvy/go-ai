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

type r52EndModelRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	ends []telemetry.TelemetryFinishEvent
}

func (r *r52EndModelRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	r.ends = append(r.ends, e)
	r.mu.Unlock()
}

// TS onEndEvent.model is the final step's model, so a PrepareStep model
// override shows on the end telemetry event.
func TestR52EndTelemetryReportsFinalStepModel(t *testing.T) {
	stepModel := &testutil.MockLanguageModel{
		ProviderName: "step-provider", ModelName: "step-model",
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
	prepare := func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
		o.Model = stepModel
		return o
	}
	for _, stream := range []bool{false, true} {
		rec := &r52EndModelRecorder{}
		settings := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}}
		base := &testutil.MockLanguageModel{ProviderName: "base-provider", ModelName: "base-model"}
		if stream {
			r, err := StreamText(context.Background(), StreamTextOptions{Model: base, Prompt: "hi", PrepareStep: prepare, Telemetry: settings})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: base, Prompt: "hi", PrepareStep: prepare, Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
		rec.mu.Lock()
		if len(rec.ends) != 1 || rec.ends[0].ModelProvider != "step-provider" || rec.ends[0].ModelID != "step-model" {
			t.Errorf("stream=%v: end events = %+v", stream, rec.ends)
		}
		rec.mu.Unlock()
	}
}

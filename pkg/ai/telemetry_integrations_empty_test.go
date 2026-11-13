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

type startCounter struct {
	telemetry.NoopTelemetryIntegration
	mu sync.Mutex
	n  int
}

func (c *startCounter) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return ctx
}

func (c *startCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// Owner decision (2026-10-28, TS `telemetry?.integrations != null ?
// asArray(integrations) : globals`): nil per-call Integrations use the global
// integrations, an empty non-nil slice means none for that call, and a
// non-empty slice replaces the globals.
func TestTelemetryIntegrationsEmptySliceMeansNone(t *testing.T) {
	global := &startCounter{}
	telemetry.RegisterTelemetryIntegration(global)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	for _, stream := range []bool{false, true} {
		local := &startCounter{}
		for _, tc := range []struct {
			name              string
			tel               *TelemetrySettings
			wantGlobal, wantL int
		}{
			{"nil settings", nil, 1, 0},
			{"nil integrations", &TelemetrySettings{}, 1, 0},
			{"empty integrations", &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{}}, 0, 0},
			{"local integrations", &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{local}}, 0, 1},
		} {
			g0, l0 := global.count(), local.count()
			if stream {
				r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "go", Telemetry: tc.tel})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.ReadAll(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "go", Telemetry: tc.tel}); err != nil {
				t.Fatal(err)
			}
			if g, l := global.count()-g0, local.count()-l0; g != tc.wantGlobal || l != tc.wantL {
				t.Errorf("stream=%v %s: global starts %d, local %d; want %d, %d", stream, tc.name, g, l, tc.wantGlobal, tc.wantL)
			}
		}
	}
	// The low-memory stream retention follows the same rule: no integrations
	// for the call means no telemetry content to keep.
	if lowMemoryNeedsContent(StreamTextOptions{Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{}}}) {
		t.Error("empty integrations keep content for telemetry")
	}
	if !lowMemoryNeedsContent(StreamTextOptions{Telemetry: &TelemetrySettings{}}) {
		t.Error("nil integrations ignore the global integration")
	}
}

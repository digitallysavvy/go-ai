package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type loopStartCounter struct {
	telemetry.NoopTelemetryIntegration
	mu sync.Mutex
	n  int
}

func (c *loopStartCounter) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return ctx
}

func (c *loopStartCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// The legacy ToolLoopAgent loop resolves integrations like GenerateText: nil
// uses the globals, an empty non-nil slice means none, a list replaces them.
// A PrepareStep override from nil to [] is a switch of integrations too.
func TestLegacyLoopEmptyIntegrationsMeansNone(t *testing.T) {
	global := &loopStartCounter{}
	telemetry.RegisterTelemetryIntegration(global)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}
	local := &loopStartCounter{}
	for _, tc := range []struct {
		name              string
		tel               *ai.TelemetrySettings
		wantGlobal, wantL int
	}{
		{"nil", nil, 1, 0},
		{"empty", &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{}}, 0, 0},
		{"local", &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{local}}, 0, 1},
	} {
		g0, l0 := global.count(), local.count()
		a := NewToolLoopAgent(AgentConfig{Model: model, Telemetry: tc.tel})
		if _, err := a.Execute(context.Background(), "go"); err != nil {
			t.Fatal(err)
		}
		if g, l := global.count()-g0, local.count()-l0; g != tc.wantGlobal || l != tc.wantL {
			t.Errorf("%s: global starts %d, local %d; want %d, %d", tc.name, g, l, tc.wantGlobal, tc.wantL)
		}
	}
	if sameIntegrations(&ai.TelemetrySettings{}, &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{}}) {
		t.Error("nil and empty integrations compare equal")
	}
	if !sameIntegrations(nil, &ai.TelemetrySettings{}) {
		t.Error("nil settings and nil integrations differ")
	}
}

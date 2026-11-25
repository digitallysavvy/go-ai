package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r68LoopStepErrorTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu      sync.Mutex
	stepErr error
	rootErr error
}

func (r *r68LoopStepErrorTelemetry) OnStepError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stepErr = e.Error
}

func (r *r68LoopStepErrorTelemetry) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rootErr = e.Error
}

// TS generateText notifies telemetry onStepError with wrapGatewayError(error)
// and telemetry onError with the original error; the AgentResult loop passed
// the original error to both.
func TestR68LegacyLoopStepErrorTelemetryGetsWrappedGatewayError(t *testing.T) {
	t.Setenv("NODE_ENV", "")
	authErr := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	rec := &r68LoopStepErrorTelemetry{}
	agent := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, authErr
			},
		},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if _, err := agent.Execute(context.Background(), "q"); err == nil {
		t.Fatal("expected an error")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.stepErr == nil || !strings.Contains(rec.stepErr.Error(), "Unauthenticated request to AI Gateway") {
		t.Fatalf("telemetry OnStepError got %v, want the wrapped gateway error", rec.stepErr)
	}
	if rec.rootErr == nil || strings.Contains(rec.rootErr.Error(), "Unauthenticated request to AI Gateway") {
		t.Fatalf("telemetry OnError got %v, want the original error", rec.rootErr)
	}
}

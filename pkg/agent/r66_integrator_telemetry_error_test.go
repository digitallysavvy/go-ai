package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r66ErrorRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	errs []error
}

func (r *r66ErrorRecorder) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.errs = append(r.errs, e.Error)
	r.mu.Unlock()
}

// TS generateText reports the original error to telemetry onError
// (`telemetryDispatcher.onError({ callId, error })`) and throws
// wrapGatewayError(error): the legacy loop's telemetry gets the unwrapped
// gateway error while the caller gets the wrapped one.
func TestR66IntegratorLegacyLoopTelemetryGetsOriginalGatewayError(t *testing.T) {
	raw := gatewayerrors.NewGatewayAuthenticationError("", 401, nil, "")
	zero := 0
	rec := &r66ErrorRecorder{}
	_, err := NewToolLoopAgent(AgentConfig{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, raw
		}},
		MaxRetries: &zero,
		Telemetry:  &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err == nil || err.Error() == raw.Error() {
		t.Fatalf("returned error = %v, want the wrapped gateway error", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errs) != 1 {
		t.Fatalf("telemetry errors = %d, want 1", len(rec.errs))
	}
	if got := rec.errs[0].Error(); got == err.Error() || got != "step 1 failed: "+raw.Error() {
		t.Fatalf("telemetry error = %q, want the original (unwrapped) error", got)
	}
}

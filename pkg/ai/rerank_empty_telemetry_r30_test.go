package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type emptyRerankRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts []string
	ends   []string
}

func (r *emptyRerankRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	r.starts = append(r.starts, e.OperationType)
	r.mu.Unlock()
	return ctx
}

func (r *emptyRerankRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	r.ends = append(r.ends, e.OperationType)
	r.mu.Unlock()
}

// TS notifies telemetryDispatcher.onStart/onEnd for an empty documents list
// too, so registered integrations see the operation.
func TestRerankEmptyDocumentsNotifiesTelemetryIntegrations(t *testing.T) {
	rec := &emptyRerankRecorder{}
	telemetry.RegisterTelemetryIntegration(rec)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     &testutil.MockRerankingModel{ProviderName: "p", ModelName: "m"},
		Documents: []string{},
		Query:     "q",
		Telemetry: &TelemetrySettings{IsEnabled: telemetry.Bool(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.starts) != 1 || rec.starts[0] != "ai.rerank" || len(rec.ends) != 1 || rec.ends[0] != "ai.rerank" {
		t.Fatalf("starts=%v ends=%v, want one ai.rerank each", rec.starts, rec.ends)
	}
}

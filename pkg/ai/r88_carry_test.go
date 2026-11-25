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

// TS 8da48db187: the ai:telemetry channel gets events filtered by the
// recording flags, while application callbacks keep their full content. The
// embed / rerank callback events Go publishes on the diagnostic channel go
// through the same filter.
func TestR88carryEmbedRerankDiagnosticsFiltered(t *testing.T) {
	var mu sync.Mutex
	var seen []interface{}
	unsub := telemetry.SubscribeDiagnostic(func(_ context.Context, m telemetry.DiagnosticMessage) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, m.Event)
		return nil
	})
	defer unsub()

	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{Embedding: []float64{0.5}, Response: types.EmbeddingResponse{Body: "raw-body"}}, nil
		},
	}
	var callbackValues []string
	_, err := Embed(context.Background(), EmbedOptions{
		Model: model, Input: "r88carry-secret-value",
		Telemetry: &telemetry.Options{RecordInputs: telemetry.Bool(false), RecordOutputs: telemetry.Bool(false), Integrations: []telemetry.TelemetryIntegration{}},
		OnStart:   func(e EmbedOnStartEvent) { callbackValues = e.Values },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(callbackValues) != 1 || callbackValues[0] != "r88carry-secret-value" {
		t.Errorf("application callback lost its input: %v", callbackValues)
	}

	rr := &testutil.MockRerankingModel{
		DoRerankFunc: func(context.Context, *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 0.9}}}, nil
		},
	}
	if _, err := Rerank(context.Background(), RerankOptions{
		Model: rr, Query: "r88carry-secret-query", Documents: []string{"r88carry-secret-doc"},
		Telemetry: &telemetry.Options{RecordInputs: telemetry.Bool(false), Integrations: []telemetry.TelemetryIntegration{}},
	}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawEmbedStart, sawEmbedEnd, sawRerankStart, sawRerankEnd bool
	for _, ev := range seen {
		switch e := ev.(type) {
		case EmbedOnStartEvent:
			sawEmbedStart = true
			if len(e.Values) != 0 {
				t.Errorf("embed start values reached the channel: %v", e.Values)
			}
		case EmbedOnFinishEvent:
			sawEmbedEnd = true
			if len(e.Value) != 0 || len(e.Embeddings) != 0 {
				t.Errorf("embed end content reached the channel: %+v", e)
			}
			for _, r := range e.Responses {
				if r.Body != nil {
					t.Errorf("embed response body reached the channel")
				}
			}
		case RerankOnStartEvent:
			sawRerankStart = true
			if e.Query != "" {
				t.Errorf("rerank query reached the channel")
			}
			if docs, ok := e.Documents.([]string); !ok || len(docs) != 0 {
				t.Errorf("rerank documents reached the channel: %v", e.Documents)
			}
		case RerankOnFinishEvent:
			sawRerankEnd = true
			if e.Query != "" || e.Result != nil {
				t.Errorf("rerank end inputs reached the channel: %+v", e)
			}
			for _, item := range e.Ranking {
				if item.Document != nil && item.Document != "" {
					t.Errorf("rerank ranking document reached the channel: %v", item.Document)
				}
			}
		}
	}
	if !sawEmbedStart || !sawEmbedEnd || !sawRerankStart || !sawRerankEnd {
		t.Errorf("events seen: embed %v/%v rerank %v/%v", sawEmbedStart, sawEmbedEnd, sawRerankStart, sawRerankEnd)
	}
}

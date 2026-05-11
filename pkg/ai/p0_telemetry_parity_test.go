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

type p0TelemetryCapture struct {
	telemetry.NoopTelemetryIntegration

	mu             sync.Mutex
	starts         []telemetry.TelemetryStartEvent
	finishes       []telemetry.TelemetryFinishEvent
	embedStarts    []telemetry.EmbeddingModelCallStartEvent
	embedFinishes  []telemetry.EmbeddingModelCallEndEvent
	rerankStarts   []telemetry.RerankingModelCallStartEvent
	rerankFinishes []telemetry.RerankingModelCallEndEvent
}

func (c *p0TelemetryCapture) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts = append(c.starts, e)
	return ctx
}

func (c *p0TelemetryCapture) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishes = append(c.finishes, e)
}

func (c *p0TelemetryCapture) OnEmbedStart(_ context.Context, e telemetry.EmbeddingModelCallStartEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.embedStarts = append(c.embedStarts, e)
}

func (c *p0TelemetryCapture) OnEmbedFinish(_ context.Context, e telemetry.EmbeddingModelCallEndEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.embedFinishes = append(c.embedFinishes, e)
}

func (c *p0TelemetryCapture) OnRerankStart(_ context.Context, e telemetry.RerankingModelCallStartEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rerankStarts = append(c.rerankStarts, e)
}

func (c *p0TelemetryCapture) OnRerankFinish(_ context.Context, e telemetry.RerankingModelCallEndEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rerankFinishes = append(c.rerankFinishes, e)
}

func TestP0Telemetry_EmbedUsesGlobalIntegrationByDefault(t *testing.T) {
	capture := &p0TelemetryCapture{}
	telemetry.RegisterTelemetryIntegration(capture)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-embedding",
	}

	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hello",
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.starts) != 1 || capture.starts[0].OperationType != "ai.embed" {
		t.Fatalf("generic starts = %#v", capture.starts)
	}
	if len(capture.embedStarts) != 1 || capture.embedStarts[0].OperationID != "ai.embed.doEmbed" {
		t.Fatalf("embed starts = %#v", capture.embedStarts)
	}
	if len(capture.embedFinishes) != 1 || capture.embedFinishes[0].Usage.TotalTokens == 0 {
		t.Fatalf("embed finishes = %#v", capture.embedFinishes)
	}
	if len(capture.finishes) != 1 {
		t.Fatalf("generic finishes = %#v", capture.finishes)
	}
	if capture.finishes[0].ModelProvider != "test-provider" || capture.finishes[0].ModelID != "test-embedding" {
		t.Fatalf("finish model identity = %s/%s", capture.finishes[0].ModelProvider, capture.finishes[0].ModelID)
	}
}

func TestP0Telemetry_RerankPerCallIntegrationOverridesGlobal(t *testing.T) {
	global := &p0TelemetryCapture{}
	local := &p0TelemetryCapture{}
	telemetry.RegisterTelemetryIntegration(global)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	model := &testutil.MockRerankingModel{
		ProviderName: "test-provider",
		ModelName:    "test-rerank",
		DoRerankFunc: func(_ context.Context, _ *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
			}, nil
		},
	}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Query:     "query",
		Documents: []string{"doc"},
		Telemetry: &telemetry.Options{
			Integrations: []telemetry.TelemetryIntegration{local},
		},
	})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	global.mu.Lock()
	globalStarts := len(global.starts)
	global.mu.Unlock()
	if globalStarts != 0 {
		t.Fatalf("global integration should be skipped by per-call telemetry, got %d starts", globalStarts)
	}

	local.mu.Lock()
	defer local.mu.Unlock()
	if len(local.starts) != 1 || local.starts[0].OperationType != "ai.rerank" {
		t.Fatalf("generic starts = %#v", local.starts)
	}
	if len(local.rerankStarts) != 1 || local.rerankStarts[0].DocumentsType != "text" {
		t.Fatalf("rerank starts = %#v", local.rerankStarts)
	}
	if len(local.rerankFinishes) != 1 || len(local.rerankFinishes[0].Ranking) != 1 {
		t.Fatalf("rerank finishes = %#v", local.rerankFinishes)
	}
	if len(local.finishes) != 1 {
		t.Fatalf("generic finishes = %#v", local.finishes)
	}
	if local.finishes[0].ModelProvider != "test-provider" || local.finishes[0].ModelID != "test-rerank" {
		t.Fatalf("finish model identity = %s/%s", local.finishes[0].ModelProvider, local.finishes[0].ModelID)
	}
}

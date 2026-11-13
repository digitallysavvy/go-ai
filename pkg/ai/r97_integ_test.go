package ai

import (
	"context"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR97integRerankProviderMetadataNonFinite (cr-r97-stream-to-integrator
// item 2; gen's duplicate CR): a reranking model's provider metadata holding
// +Inf (a provider's 1e400, ruling 4) reaches the finish event, written the
// JSON.stringify way (null); json.Marshal dropped it whole.
func TestR97integRerankProviderMetadataNonFinite(t *testing.T) {
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking:          []types.RerankItem{{Index: 0, RelevanceScore: 1.0}},
				ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"x": math.Inf(1), "k": "v"}},
			}, nil
		},
	}
	var got string
	_, err := Rerank(context.Background(), RerankOptions{
		Model: model, Documents: []string{"doc1"}, Query: "q",
		OnEnd: func(e RerankOnFinishEvent) { got = string(e.ProviderMetadata) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"p":{"k":"v","x":null}}` {
		t.Fatalf("event providerMetadata = %q, want {\"p\":{\"k\":\"v\",\"x\":null}}", got)
	}
}

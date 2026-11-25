package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A model response without a timestamp or model id gets the call time and the
// model's id, as in TS rerank().
func TestRerankFillsBlankResponseMetadata(t *testing.T) {
	t.Parallel()
	model := &testutil.MockRerankingModel{
		ModelName: "rerank-x",
		DoRerankFunc: func(context.Context, *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 1}}}, nil
		},
	}
	var finished RerankOnFinishEvent
	result, err := Rerank(context.Background(), RerankOptions{
		Model: model, Query: "q", Documents: []string{"a"},
		OnEnd: func(e RerankOnFinishEvent) { finished = e },
	})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if result.Response.ModelID != "rerank-x" || result.Response.Timestamp.IsZero() {
		t.Fatalf("Response = %+v, want model id and timestamp filled", result.Response)
	}
	if finished.Response.ModelID != "rerank-x" || finished.Response.Timestamp.IsZero() {
		t.Fatalf("OnEnd Response = %+v, want model id and timestamp filled", finished.Response)
	}
}

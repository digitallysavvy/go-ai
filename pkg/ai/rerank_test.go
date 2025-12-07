package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestRerank_Basic(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 1, RelevanceScore: 0.9},
					{Index: 0, RelevanceScore: 0.5},
					{Index: 2, RelevanceScore: 0.3},
				},
				Response: types.RerankResponse{ModelID: "mock-reranking"},
			}, nil
		},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1", "doc2", "doc3"},
		Query:     "search query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Ranking) != 3 {
		t.Errorf("expected 3 ranked items, got %d", len(result.Ranking))
	}
	if result.Ranking[0].OriginalIndex != 1 {
		t.Errorf("expected first ranked item to be index 1, got %d", result.Ranking[0].OriginalIndex)
	}
	if result.Ranking[0].Score != 0.9 {
		t.Errorf("expected first score 0.9, got %f", result.Ranking[0].Score)
	}
}

func TestRerank_NilModel(t *testing.T) {
	t.Parallel()

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     nil,
		Documents: []string{"doc1"},
		Query:     "query",
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if err.Error() != "model is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRerank_NilDocuments(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: nil,
		Query:     "query",
	})

	if err == nil {
		t.Fatal("expected error for nil documents")
	}
	if err.Error() != "documents are required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRerank_EmptyQuery(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1"},
		Query:     "",
	})

	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if err.Error() != "query is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRerank_EmptyDocuments(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{},
		Query:     "query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Ranking) != 0 {
		t.Errorf("expected empty ranking, got %d items", len(result.Ranking))
	}
}

func TestRerank_StringDocuments(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			docs, ok := opts.Documents.([]string)
			if !ok {
				t.Error("expected []string documents")
			}
			if len(docs) != 2 {
				t.Errorf("expected 2 documents, got %d", len(docs))
			}
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 0, RelevanceScore: 0.8},
					{Index: 1, RelevanceScore: 0.6},
				},
			}, nil
		},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1", "doc2"},
		Query:     "query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Ranking) != 2 {
		t.Errorf("expected 2 ranked items, got %d", len(result.Ranking))
	}
}

func TestRerank_MapDocuments(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 0, RelevanceScore: 0.9},
				},
			}, nil
		},
	}

	docs := []map[string]interface{}{
		{"title": "Doc 1", "content": "Content 1"},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: docs,
		Query:     "query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Ranking) != 1 {
		t.Errorf("expected 1 ranked item, got %d", len(result.Ranking))
	}
}

func TestRerank_TopN(t *testing.T) {
	t.Parallel()

	topN := 2
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			if opts.TopN == nil || *opts.TopN != topN {
				t.Errorf("expected TopN %d, got %v", topN, opts.TopN)
			}
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 1, RelevanceScore: 0.9},
					{Index: 0, RelevanceScore: 0.5},
				},
			}, nil
		},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1", "doc2", "doc3"},
		Query:     "query",
		TopN:      &topN,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Ranking) != 2 {
		t.Errorf("expected 2 ranked items, got %d", len(result.Ranking))
	}
}

func TestRerank_OnFinishCallback(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 1.0}},
			}, nil
		},
	}

	finishCalled := false
	var capturedResult *RerankResult

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1"},
		Query:     "query",
		OnFinish: func(result *RerankResult) {
			finishCalled = true
			capturedResult = result
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !finishCalled {
		t.Error("expected OnFinish callback to be called")
	}
	if capturedResult == nil {
		t.Error("callback did not receive result")
	}
}

func TestRerank_InvalidDocumentType(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: 12345, // Invalid type
		Query:     "query",
	})

	if err == nil {
		t.Fatal("expected error for invalid document type")
	}
}

func TestRerank_RerankError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("reranking failed")
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return nil, expectedErr
		},
	}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1"},
		Query:     "query",
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestRerank_RerankedDocuments(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 2, RelevanceScore: 0.9},
					{Index: 0, RelevanceScore: 0.5},
					{Index: 1, RelevanceScore: 0.3},
				},
			}, nil
		},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"first", "second", "third"},
		Query:     "query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RerankedDocuments should be in order of relevance
	reranked := result.RerankedDocuments.([]interface{})
	if reranked[0] != "third" {
		t.Errorf("expected first reranked doc to be 'third', got %v", reranked[0])
	}
	if reranked[1] != "first" {
		t.Errorf("expected second reranked doc to be 'first', got %v", reranked[1])
	}
	if reranked[2] != "second" {
		t.Errorf("expected third reranked doc to be 'second', got %v", reranked[2])
	}
}

func TestRerank_InterfaceDocuments(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 0, RelevanceScore: 0.8},
				},
			}, nil
		},
	}

	docs := []interface{}{"doc1"}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: docs,
		Query:     "query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Ranking) != 1 {
		t.Errorf("expected 1 ranked item, got %d", len(result.Ranking))
	}
}

func TestRerank_ProviderMetadata(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking:          []types.RerankItem{{Index: 0, RelevanceScore: 1.0}},
				ProviderMetadata: map[string]interface{}{"custom": "data"},
			}, nil
		},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1"},
		Query:     "query",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Error("expected provider metadata")
	}
}

func TestTimeNow(t *testing.T) {
	// Test that the timeNow helper works
	now := timeNow()
	if now.IsZero() {
		t.Error("timeNow returned zero time")
	}
	if now.After(time.Now().Add(time.Second)) {
		t.Error("timeNow returned future time")
	}
}

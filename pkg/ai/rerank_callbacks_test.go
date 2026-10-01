package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestRerankOnStartCallbackFired verifies that ExperimentalOnStart is called
// before the reranking model is invoked.
func TestRerankOnStartCallbackFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	var mu sync.Mutex
	var captured *RerankOnStartEvent

	docs := []string{"doc1", "doc2", "doc3"}
	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: docs,
		Query:     "test query",
		ExperimentalOnStart: func(e RerankOnStartEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if e.Provider != "test-provider" {
		t.Errorf("Provider = %q, want %q", e.Provider, "test-provider")
	}
	if e.ModelID != "test-model" {
		t.Errorf("ModelID = %q, want %q", e.ModelID, "test-model")
	}
	if e.Query != "test query" {
		t.Errorf("Query = %q, want %q", e.Query, "test query")
	}
}

// TestRerankOnStartHeadersAndMaxRetries verifies that MaxRetries and Headers from
// RerankOptions are forwarded to RerankOnStartEvent, and Headers are threaded
// through to the provider call.
func TestRerankOnStartHeadersAndMaxRetries(t *testing.T) {
	t.Parallel()

	var capturedProviderOpts *provider.RerankOptions
	model := &testutil.MockRerankingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoRerankFunc: func(_ context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			capturedProviderOpts = opts
			return &types.RerankResult{
				Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
			}, nil
		},
	}

	var mu sync.Mutex
	var captured *RerankOnStartEvent

	_, err := Rerank(context.Background(), RerankOptions{
		Model:      model,
		Documents:  []string{"doc1"},
		Query:      "query",
		MaxRetries: intPtr(2),
		Headers:    map[string]string{"X-Custom": "hdr"},
		ExperimentalOnStart: func(e RerankOnStartEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if e.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", e.MaxRetries)
	}
	if e.Headers["X-Custom"] != "hdr" {
		t.Errorf("Headers[X-Custom] = %q, want %q", e.Headers["X-Custom"], "hdr")
	}
	// Headers must be threaded through to the provider call.
	if capturedProviderOpts == nil || capturedProviderOpts.Headers["X-Custom"] != "hdr" {
		t.Error("Headers not threaded through to provider.RerankOptions")
	}
}

// TestRerankOnFinishWarningsForwarded verifies that provider warnings are
// propagated through to RerankOnFinishEvent.Warnings.
func TestRerankOnFinishWarningsForwarded(t *testing.T) {
	t.Parallel()

	warn := types.Warning{Type: "unsupported-setting", Message: "topN ignored"}
	model := &testutil.MockRerankingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoRerankFunc: func(_ context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking:  []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
				Warnings: []types.Warning{warn},
			}, nil
		},
	}

	var mu sync.Mutex
	var captured *RerankOnFinishEvent

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1"},
		Query:     "query",
		ExperimentalOnFinish: func(e RerankOnFinishEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnFinish was not called")
	}
	if len(e.Warnings) != 1 {
		t.Fatalf("Warnings count = %d, want 1", len(e.Warnings))
	}
	if e.Warnings[0].Type != warn.Type || e.Warnings[0].Message != warn.Message {
		t.Errorf("Warnings[0] = %+v, want %+v", e.Warnings[0], warn)
	}
}

// TestRerankOnFinishCallbackFired verifies that ExperimentalOnFinish is called
// after the reranking model returns with the full result.
func TestRerankOnFinishCallbackFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	var mu sync.Mutex
	var captured *RerankOnFinishEvent

	docs := []string{"doc1", "doc2"}
	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: docs,
		Query:     "query",
		ExperimentalOnFinish: func(e RerankOnFinishEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnFinish was not called")
	}
	if e.Provider != "test-provider" {
		t.Errorf("Provider = %q, want %q", e.Provider, "test-provider")
	}
	if e.Result == nil {
		t.Fatal("ExperimentalOnFinish.Result should not be nil")
	}
}

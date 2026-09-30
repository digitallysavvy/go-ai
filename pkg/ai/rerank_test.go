package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

// TestRerank_InvalidRankingIndexRejected mirrors TS rerank.test.ts
// "should reject invalid provider ranking index %s" (index 3, -1, 5 for 3
// documents): the call must fail with an InvalidResponseDataError, must not
// retry, and must not fire onEnd. It must also not panic when indexing
// documentsSlice.
func TestRerank_InvalidRankingIndexRejected(t *testing.T) {
	t.Parallel()

	for _, idx := range []int{3, -1, 5} {
		idx := idx
		t.Run("", func(t *testing.T) {
			t.Parallel()

			calls := 0
			onEndCalled := false
			model := &testutil.MockRerankingModel{
				DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
					calls++
					return &types.RerankResult{
						Ranking: []types.RerankItem{{Index: idx, RelevanceScore: 0.9}},
					}, nil
				},
			}

			_, err := Rerank(context.Background(), RerankOptions{
				Model:     model,
				Documents: []string{"a", "b", "c"},
				Query:     "q",
				ExperimentalOnEnd: func(RerankOnFinishEvent) {
					onEndCalled = true
				},
			})

			if err == nil {
				t.Fatal("expected error for invalid ranking index")
			}
			if !providererrors.IsInvalidResponseDataError(err) {
				t.Errorf("expected InvalidResponseDataError, got: %v (%T)", err, err)
			}
			wantMsg := "Invalid ranking index"
			if !strings.Contains(err.Error(), wantMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), wantMsg)
			}
			if calls != 1 {
				t.Errorf("doRerank calls = %d, want 1 (no retry for a validation error)", calls)
			}
			if onEndCalled {
				t.Error("onEnd must not be called when the ranking index is invalid")
			}
		})
	}
}

// TestRerank_EmptyDocumentsFiresCallbacks mirrors TS rerank.test.ts
// "should fire callbacks for empty documents".
func TestRerank_EmptyDocumentsFiresCallbacks(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{}

	var startEvent *RerankOnStartEvent
	var endEvent *RerankOnFinishEvent

	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{},
		Query:     "rainy day",
		ExperimentalOnStart: func(e RerankOnStartEvent) {
			startEvent = &e
		},
		ExperimentalOnEnd: func(e RerankOnFinishEvent) {
			endEvent = &e
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if startEvent == nil {
		t.Fatal("expected onStart to be called for empty documents")
	}
	if endEvent == nil {
		t.Fatal("expected onEnd to be called for empty documents")
	}
	if len(endEvent.Ranking) != 0 {
		t.Errorf("expected empty ranking in onEnd, got %d items", len(endEvent.Ranking))
	}
}

// TestRerank_RetriesRetryableError mirrors the embed/embedMany retry policy:
// a retryable provider error (429) is retried until it succeeds, using the
// default MaxRetries (nil -> 2, matching TS prepareRetries).
func TestRerank_RetriesRetryableError(t *testing.T) {
	t.Parallel()

	attempts := 0
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			attempts++
			if attempts < 2 {
				pe := providererrors.NewProviderError("mock", 429, "RESOURCE_EXHAUSTED", "rate limited", nil)
				pe.ResponseHeaders = map[string]string{"retry-after-ms": "0"}
				return nil, pe
			}
			return &types.RerankResult{
				Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
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
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if len(result.Ranking) != 1 {
		t.Errorf("expected 1 ranked item, got %d", len(result.Ranking))
	}
}

// TestRerank_RetriedAttemptSpanEndsWithErrorStatus is the regression test
// for the "rerank.go rerankSpan overwrite on retry" fix: a failed attempt's
// nested "ai.rerank.doRerank" span must end (with error status) even though
// a later retry of the same call succeeds — previously rerank.go never
// notified onRerankEnd for a failed attempt (mirroring a real gap in TS's
// own rerank.ts/open-telemetry.ts), so the failed attempt's span was
// unconditionally overwritten and orphaned by the next attempt's
// OnRerankStart (state.rerankSpan/st.rerankSpan is a single field, not a
// per-attempt map like embed's state.embedSpans), leaking it forever.
// Reuses the retry scenario from TestRerank_RetriesRetryableError but with
// a span recorder attached, asserting every started "ai.rerank.doRerank"
// span appears in Ended() exactly once, with the correct status per
// attempt.
func TestRerank_RetriedAttemptSpanEndsWithErrorStatus(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("rerank-retry-span-test")

	attempts := 0
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			attempts++
			if attempts < 2 {
				pe := providererrors.NewProviderError("mock", 429, "RESOURCE_EXHAUSTED", "rate limited", nil)
				pe.ResponseHeaders = map[string]string{"retry-after-ms": "0"}
				return nil, pe
			}
			return &types.RerankResult{
				Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
			}, nil
		},
	}

	result, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Documents: []string{"doc1"},
		Query:     "query",
		Telemetry: &TelemetrySettings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(result.Ranking) != 1 {
		t.Fatalf("expected 1 ranked item, got %d", len(result.Ranking))
	}

	// GenAI's root "ai.rerank" span is also present, so restrict to spans
	// with a valid parent (the nested "ai.rerank.doRerank" attempt spans).
	var doRerankSpans []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if strings.HasPrefix(s.Name(), "rerank") && s.Parent().SpanID().IsValid() {
			doRerankSpans = append(doRerankSpans, s)
		}
	}
	if len(doRerankSpans) != 2 {
		t.Fatalf("expected 2 ended doRerank attempt spans (one per attempt, including the failed one that was retried), got %d", len(doRerankSpans))
	}
	var errored, notErrored int
	for _, s := range doRerankSpans {
		if s.Status().Code == codes.Error {
			errored++
		} else {
			notErrored++
		}
	}
	if errored != 1 {
		t.Fatalf("expected exactly 1 span with error status (the failed attempt), got %d", errored)
	}
	if notErrored != 1 {
		t.Fatalf("expected exactly 1 span without error status (the successful retry), got %d", notErrored)
	}
}

// TestRerank_RejectsNegativeMaxRetries mirrors TS prepareRetries: a negative
// maxRetries is an InvalidArgumentError.
func TestRerank_RejectsNegativeMaxRetries(t *testing.T) {
	t.Parallel()

	model := &testutil.MockRerankingModel{}
	negative := -1

	_, err := Rerank(context.Background(), RerankOptions{
		Model:      model,
		Documents:  []string{"doc1"},
		Query:      "query",
		MaxRetries: &negative,
	})
	if err == nil {
		t.Fatal("expected error for negative MaxRetries")
	}
}

// TestRerank_RuntimeContextThreaded verifies RuntimeContext flows unchanged
// to both the start and end callbacks.
func TestRerank_RuntimeContextThreaded(t *testing.T) {
	t.Parallel()

	type ctxKey struct{ UserID string }
	rc := ctxKey{UserID: "u-1"}

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
			}, nil
		},
	}

	var startRC, endRC interface{}
	_, err := Rerank(context.Background(), RerankOptions{
		Model:          model,
		Documents:      []string{"doc1"},
		Query:          "query",
		RuntimeContext: rc,
		ExperimentalOnStart: func(e RerankOnStartEvent) {
			startRC = e.RuntimeContext
		},
		ExperimentalOnEnd: func(e RerankOnFinishEvent) {
			endRC = e.RuntimeContext
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if startRC != rc {
		t.Errorf("start RuntimeContext = %#v, want %#v", startRC, rc)
	}
	if endRC != rc {
		t.Errorf("end RuntimeContext = %#v, want %#v", endRC, rc)
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

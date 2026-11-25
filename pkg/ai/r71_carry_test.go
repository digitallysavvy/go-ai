package ai

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 71 carry ports from packages/ai/src/rerank/rerank.test.ts,
// embed/embed.test.ts and embed/embed-many.test.ts (ai@e64ed82f27).

var r71carryRerankDocs = []string{
	"sunny day at the beach",
	"rainy day in the city",
	"cloudy day in the mountains",
}

func r71carryRerankModel(calls *[]string, mu *sync.Mutex) *testutil.MockRerankingModel {
	return &testutil.MockRerankingModel{
		DoRerankFunc: func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
			if calls != nil {
				mu.Lock()
				*calls = append(*calls, "doRerank")
				mu.Unlock()
			}
			return &types.RerankResult{Ranking: []types.RerankItem{
				{Index: 2, RelevanceScore: 0.9},
				{Index: 0, RelevanceScore: 0.8},
				{Index: 1, RelevanceScore: 0.7},
			}}, nil
		},
	}
}

// rerank.test.ts "should isolate the result from ranking mutations in onEnd".
func TestR71CarryRerankOnEndRankingMutationIsolated(t *testing.T) {
	t.Parallel()
	result, err := Rerank(context.Background(), RerankOptions{
		Model:     r71carryRerankModel(nil, nil),
		Documents: r71carryRerankDocs,
		Query:     "rainy day",
		OnEnd: func(event RerankOnFinishEvent) {
			event.Ranking[0].Document = "mutated document"
			event.Ranking = append(event.Ranking, RerankItem{OriginalIndex: 0, Score: 0, Document: "appended document"})
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	want := []RerankItem{
		{OriginalIndex: 2, Score: 0.9, Document: "cloudy day in the mountains"},
		{OriginalIndex: 0, Score: 0.8, Document: "sunny day at the beach"},
		{OriginalIndex: 1, Score: 0.7, Document: "rainy day in the city"},
	}
	if !reflect.DeepEqual(result.Ranking, want) {
		t.Fatalf("result.Ranking = %#v, want %#v", result.Ranking, want)
	}
	wantDocs := []interface{}{"cloudy day in the mountains", "sunny day at the beach", "rainy day in the city"}
	if !reflect.DeepEqual(result.RerankedDocuments, wantDocs) {
		t.Fatalf("result.RerankedDocuments = %#v, want %#v", result.RerankedDocuments, wantDocs)
	}
}

// rerank.test.ts "should reject invalid provider ranking index %s" (the
// non-integer 1.5 case cannot occur with a Go int index).
func TestR71CarryRerankRejectsInvalidRankingIndex(t *testing.T) {
	t.Parallel()
	for _, index := range []int{3, -1, 5} {
		ranking := []types.RerankItem{{Index: index, RelevanceScore: 0.9}}
		calls := 0
		endCalled := false
		_, err := Rerank(context.Background(), RerankOptions{
			Model: &testutil.MockRerankingModel{DoRerankFunc: func(context.Context, *provider.RerankOptions) (*types.RerankResult, error) {
				calls++
				return &types.RerankResult{Ranking: ranking}, nil
			}},
			Documents: []string{"a", "b", "c"},
			Query:     "q",
			OnEnd:     func(RerankOnFinishEvent) { endCalled = true },
		})
		var dataErr *providererrors.InvalidResponseDataError
		if !errors.As(err, &dataErr) {
			t.Fatalf("index %d: err = %T %v, want InvalidResponseDataError", index, err, err)
		}
		wantMsg := "Invalid ranking index " + strconv.Itoa(index) + ". Expected an integer between 0 and 2."
		if dataErr.Message != wantMsg {
			t.Errorf("index %d: message = %q, want %q", index, dataErr.Message, wantMsg)
		}
		if !reflect.DeepEqual(dataErr.Data, ranking) {
			t.Errorf("index %d: data = %#v, want %#v", index, dataErr.Data, ranking)
		}
		if calls != 1 {
			t.Errorf("index %d: doRerank calls = %d, want 1", index, calls)
		}
		if endCalled {
			t.Errorf("index %d: onEnd called", index)
		}
	}
}

// A model that returns (nil, nil) is an error, not a panic (r70 nil-result
// ruling applied to rerank).
func TestR71CarryRerankNilModelResultIsError(t *testing.T) {
	t.Parallel()
	endCalled := false
	_, err := Rerank(context.Background(), RerankOptions{
		Model: &testutil.MockRerankingModel{DoRerankFunc: func(context.Context, *provider.RerankOptions) (*types.RerankResult, error) {
			return nil, nil
		}},
		Documents: []string{"a"},
		Query:     "q",
		OnEnd:     func(RerankOnFinishEvent) { endCalled = true },
	})
	if err == nil || err.Error() != "reranking model mock/mock-reranking returned no result" {
		t.Fatalf("err = %v", err)
	}
	if endCalled {
		t.Fatal("onEnd called")
	}
}

// rerank.test.ts "options.onStart and onEnd together".
func TestR71CarryRerankCallbackOrderAndPanics(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var order []string
	var startID, endID string
	_, err := Rerank(context.Background(), RerankOptions{
		Model:     r71carryRerankModel(&order, &mu),
		Documents: r71carryRerankDocs,
		Query:     "rainy day",
		OnStart: func(e RerankOnStartEvent) {
			mu.Lock()
			order = append(order, "onStart")
			startID = e.CallID
			mu.Unlock()
		},
		OnEnd: func(e RerankOnFinishEvent) {
			mu.Lock()
			order = append(order, "onEnd")
			endID = e.CallID
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if want := []string{"onStart", "doRerank", "onEnd"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if startID == "" || startID != endID || !strings.HasPrefix(startID, "call-") {
		t.Fatalf("callIds start=%q end=%q", startID, endID)
	}

	// "should still call onEnd when onStart throws" and "should not break
	// reranking when callback throws".
	endCalled := false
	result, err := Rerank(context.Background(), RerankOptions{
		Model:     r71carryRerankModel(nil, nil),
		Documents: r71carryRerankDocs,
		Query:     "rainy day",
		OnStart:   func(RerankOnStartEvent) { panic("start error") },
		OnEnd:     func(RerankOnFinishEvent) { endCalled = true; panic("end error") },
	})
	if err != nil {
		t.Fatalf("Rerank with panicking callbacks: %v", err)
	}
	if !endCalled || len(result.Ranking) != 3 || result.Ranking[0].OriginalIndex != 2 {
		t.Fatalf("endCalled=%v ranking=%#v", endCalled, result.Ranking)
	}
}

// rerank.test.ts "should fire callbacks for empty documents".
func TestR71CarryRerankEmptyDocumentsCallbacks(t *testing.T) {
	t.Parallel()
	var start RerankOnStartEvent
	var end RerankOnFinishEvent
	calls := 0
	_, err := Rerank(context.Background(), RerankOptions{
		Model: &testutil.MockRerankingModel{DoRerankFunc: func(context.Context, *provider.RerankOptions) (*types.RerankResult, error) {
			calls++
			return &types.RerankResult{}, nil
		}},
		Documents: []string{},
		Query:     "rainy day",
		OnStart:   func(e RerankOnStartEvent) { start = e },
		OnEnd:     func(e RerankOnFinishEvent) { end = e },
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if calls != 0 {
		t.Fatalf("doRerank calls = %d", calls)
	}
	if start.CallID == "" || start.CallID != end.CallID {
		t.Fatalf("callIds %q %q", start.CallID, end.CallID)
	}
	if !reflect.DeepEqual(start.Documents, []string{}) || !reflect.DeepEqual(end.Documents, []string{}) {
		t.Fatalf("documents start=%#v end=%#v", start.Documents, end.Documents)
	}
	if end.Ranking == nil || len(end.Ranking) != 0 {
		t.Fatalf("end ranking = %#v, want []", end.Ranking)
	}
}

// embed.test.ts / embed-many.test.ts "options.onStart and onEnd together".
func TestR71CarryEmbedCallbackOrderAndPanics(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var order []string
	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			mu.Lock()
			order = append(order, "doEmbed")
			mu.Unlock()
			return &types.EmbeddingResult{Embedding: []float64{0.1, 0.2}}, nil
		},
		DoEmbedManyFunc: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			mu.Lock()
			order = append(order, "doEmbed")
			mu.Unlock()
			out := make([][]float64, len(values))
			for i := range values {
				out[i] = []float64{float64(i)}
			}
			return &types.EmbeddingsResult{Embeddings: out}, nil
		},
		MaxEmbeddings: 5,
	}
	var startID, endID string
	onStart := func(e EmbedOnStartEvent) {
		mu.Lock()
		order = append(order, "onStart")
		startID = e.CallID
		mu.Unlock()
	}
	onEnd := func(e EmbedOnFinishEvent) {
		mu.Lock()
		order = append(order, "onEnd")
		endID = e.CallID
		mu.Unlock()
	}
	if _, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "v", OnStart: onStart, OnEnd: onEnd}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if want := []string{"onStart", "doEmbed", "onEnd"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("embed order = %v, want %v", order, want)
	}
	if startID == "" || startID != endID {
		t.Fatalf("embed callIds %q %q", startID, endID)
	}
	order = nil
	if _, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b", "c"}, OnStart: onStart, OnEnd: onEnd}); err != nil {
		t.Fatalf("EmbedMany: %v", err)
	}
	if want := []string{"onStart", "doEmbed", "onEnd"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("embedMany order = %v, want %v", order, want)
	}
	if startID == "" || startID != endID {
		t.Fatalf("embedMany callIds %q %q", startID, endID)
	}

	endCalled := 0
	panicStart := func(EmbedOnStartEvent) { panic("start error") }
	panicEnd := func(EmbedOnFinishEvent) { endCalled++; panic("end error") }
	res, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "v", OnStart: panicStart, OnEnd: panicEnd})
	if err != nil || !reflect.DeepEqual(res.Embedding, []float64{0.1, 0.2}) {
		t.Fatalf("Embed with panicking callbacks: %v %#v", err, res)
	}
	many, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, OnStart: panicStart, OnEnd: panicEnd})
	if err != nil || !reflect.DeepEqual(many.Embeddings, [][]float64{{0}, {1}}) {
		t.Fatalf("EmbedMany with panicking callbacks: %v %#v", err, many)
	}
	if endCalled != 2 {
		t.Fatalf("onEnd calls = %d, want 2", endCalled)
	}
}

// embed-many.test.ts "should send correct event information (chunked path)".
func TestR71CarryEmbedManyChunkedEndEvent(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &batchEmbeddingModel{
		maxPerCall: 2,
		doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			calls++
			switch calls {
			case 1:
				return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}, {0.2}}, Usage: types.EmbeddingUsage{Tokens: 10}}, nil
			case 2:
				return &types.EmbeddingsResult{Embeddings: [][]float64{{0.3}}, Usage: types.EmbeddingUsage{Tokens: 5}}, nil
			}
			return nil, errors.New("Unexpected call")
		},
	}
	values := []string{"sunny day at the beach", "rainy afternoon in the city", "snowy night in the mountains"}
	var end EmbedOnFinishEvent
	res, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: values, OnEnd: func(e EmbedOnFinishEvent) { end = e }})
	if err != nil {
		t.Fatalf("EmbedMany: %v", err)
	}
	if end.OperationID != "ai.embedMany" || !strings.HasPrefix(end.CallID, "call-") {
		t.Fatalf("operationId=%q callId=%q", end.OperationID, end.CallID)
	}
	want := [][]float64{{0.1}, {0.2}, {0.3}}
	if !reflect.DeepEqual(end.Embeddings, want) || !reflect.DeepEqual(res.Embeddings, want) {
		t.Fatalf("embeddings event=%#v result=%#v", end.Embeddings, res.Embeddings)
	}
	if end.Usage.Tokens != 15 || res.Usage.Tokens != 15 {
		t.Fatalf("usage event=%v result=%v, want 15", end.Usage.Tokens, res.Usage.Tokens)
	}
	if !reflect.DeepEqual(end.Value, values) {
		t.Fatalf("value = %#v", end.Value)
	}
	if len(end.Responses) != 2 || len(res.Responses) != 2 {
		t.Fatalf("responses event=%d result=%d, want 2", len(end.Responses), len(res.Responses))
	}
}

// embed-many.test.ts "should support maxParallelCalls": with
// maxParallelCalls 2 the third call starts only after the first two end.
func TestR71CarryEmbedManyMaxParallelCalls(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	inFlight, maxInFlight, total := 0, 0, 0
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	model := &batchEmbeddingModel{
		maxPerCall: 1,
		parallel:   true,
		doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			mu.Lock()
			inFlight++
			total++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()
			started <- struct{}{}
			<-release
			mu.Lock()
			inFlight--
			mu.Unlock()
			return &types.EmbeddingsResult{Embeddings: [][]float64{{float64(len(values[0]))}}}, nil
		},
	}
	done := make(chan *EmbedManyResult, 1)
	go func() {
		res, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "bb", "ccc"}, MaxParallelCalls: 2})
		if err != nil {
			t.Errorf("EmbedMany: %v", err)
		}
		done <- res
	}()
	<-started
	<-started
	mu.Lock()
	if total != 2 {
		t.Errorf("calls started before release = %d, want 2", total)
	}
	mu.Unlock()
	close(release)
	res := <-done
	if maxInFlight != 2 {
		t.Fatalf("max in flight = %d, want 2", maxInFlight)
	}
	if res == nil || !reflect.DeepEqual(res.Embeddings, [][]float64{{1}, {2}, {3}}) {
		t.Fatalf("embeddings = %#v", res)
	}
}

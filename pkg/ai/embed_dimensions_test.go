package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/ai/src/embed/embed-dimensions.test.ts (TS #22335).

type dimensionsRecorder struct {
	mu   sync.Mutex
	dims []*int
	opts []map[string]interface{}
}

func (r *dimensionsRecorder) wrap(m *batchEmbeddingModel) {
	m.doEmbedMany = func(_ context.Context, values []string, o *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		r.mu.Lock()
		r.dims = append(r.dims, o.Dimensions)
		r.opts = append(r.opts, o.ProviderOptions)
		r.mu.Unlock()
		emb := make([][]float64, len(values))
		for i := range values {
			emb[i] = []float64{0.1, 0.2, 0.3}
		}
		return &types.EmbeddingsResult{Embeddings: emb}, nil
	}
}

func TestEmbedDimensionsForwarded(t *testing.T) {
	cases := []struct {
		name  string
		model *batchEmbeddingModel
		run   func(m provider.EmbeddingModel, dims *int, onStart func(EmbedOnStartEvent)) error
		calls int
	}{
		{"embed", &batchEmbeddingModel{maxPerCall: 100}, func(m provider.EmbeddingModel, dims *int, onStart func(EmbedOnStartEvent)) error {
			_, err := Embed(context.Background(), EmbedOptions{Model: m, Input: "one", Dimensions: dims, OnStart: onStart, ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"dimensions": 2}}})
			return err
		}, 1},
		{"embedMany single call", &batchEmbeddingModel{maxPerCall: 100}, func(m provider.EmbeddingModel, dims *int, onStart func(EmbedOnStartEvent)) error {
			_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: m, Inputs: []string{"one", "two", "three"}, Dimensions: dims, OnStart: onStart, ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"dimensions": 2}}})
			return err
		}, 1},
		{"embedMany chunks", &batchEmbeddingModel{maxPerCall: 2}, func(m provider.EmbeddingModel, dims *int, onStart func(EmbedOnStartEvent)) error {
			_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: m, Inputs: []string{"one", "two", "three"}, Dimensions: dims, OnStart: onStart, ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"dimensions": 2}}})
			return err
		}, 2},
		{"embedMany parallel chunks", &batchEmbeddingModel{maxPerCall: 2, parallel: true}, func(m provider.EmbeddingModel, dims *int, onStart func(EmbedOnStartEvent)) error {
			_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: m, Inputs: []string{"one", "two", "three"}, Dimensions: dims, OnStart: onStart, ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"dimensions": 2}}})
			return err
		}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &dimensionsRecorder{}
			rec.wrap(tc.model)
			var started []EmbedOnStartEvent
			var mu sync.Mutex
			err := tc.run(tc.model, intPtr(3), func(e EmbedOnStartEvent) { mu.Lock(); started = append(started, e); mu.Unlock() })
			if err != nil {
				t.Fatal(err)
			}
			if len(rec.dims) != tc.calls {
				t.Fatalf("calls = %d, want %d", len(rec.dims), tc.calls)
			}
			for i, d := range rec.dims {
				if d == nil || *d != 3 {
					t.Fatalf("call %d dimensions = %v, want 3", i, d)
				}
				if rec.opts[i]["test"].(map[string]interface{})["dimensions"] != 2 {
					t.Fatalf("provider options changed: %#v", rec.opts[i])
				}
			}
			if len(started) != 1 || started[0].Dimensions == nil || *started[0].Dimensions != 3 {
				t.Fatalf("start events = %#v", started)
			}

			// Omitted dimensions stay unspecified.
			rec2 := &dimensionsRecorder{}
			rec2.wrap(tc.model)
			if err := tc.run(tc.model, nil, nil); err != nil {
				t.Fatal(err)
			}
			for _, d := range rec2.dims {
				if d != nil {
					t.Fatalf("dimensions = %d, want nil", *d)
				}
			}
		})
	}
}

func TestEmbedDimensionsRejectsInvalid(t *testing.T) {
	for _, v := range []int{0, -1} {
		model := &batchEmbeddingModel{maxPerCall: 100}
		rec := &dimensionsRecorder{}
		rec.wrap(model)
		_, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "one", Dimensions: intPtr(v)})
		if !providererrors.IsInvalidArgumentError(err) {
			t.Fatalf("Embed(%d): err = %v", v, err)
		}
		_, err = EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"one"}, Dimensions: intPtr(v)})
		if !providererrors.IsInvalidArgumentError(err) {
			t.Fatalf("EmbedMany(%d): err = %v", v, err)
		}
		if len(rec.dims) != 0 {
			t.Fatalf("model called %d times", len(rec.dims))
		}
	}
}

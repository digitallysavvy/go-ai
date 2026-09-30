package perplexity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func newCtx() context.Context { return context.Background() }

// encodeInt8 mirrors the TS test helper: encodes signed int8 values the way
// Perplexity's base64_int8 format does.
func encodeInt8(values []int8) string {
	b := make([]byte, len(values))
	for i, v := range values {
		b[i] = byte(v)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func encodeBinary(values []byte) string {
	return base64.StdEncoding.EncodeToString(values)
}

func TestPerplexityEmbeddingModelMetadataAndLimits(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewEmbeddingModel(p, "pplx-embed-v1-4b")
	if m.SpecificationVersion() != "v3" || m.Provider() != "perplexity" || m.ModelID() != "pplx-embed-v1-4b" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
	if m.MaxEmbeddingsPerCall() != 512 || !m.SupportsParallelCalls() {
		t.Fatalf("limits mismatch: max=%d parallel=%v", m.MaxEmbeddingsPerCall(), m.SupportsParallelCalls())
	}
}

func TestPerplexityEmbeddingModelDecodesInt8Embeddings(t *testing.T) {
	dummy := [][]int8{{1, -2, 3, 127, -128}, {-1, 2, -3, 100, -50}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["encoding_format"] != "base64_int8" {
			t.Fatalf("encoding_format = %v, want base64_int8 default", body["encoding_format"])
		}
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "index": 0, "embedding": encodeInt8(dummy[0])},
				{"object": "embedding", "index": 1, "embedding": encodeInt8(dummy[1])},
			},
			"model": "pplx-embed-v1-4b",
			"usage": map[string]interface{}{"prompt_tokens": 8, "total_tokens": 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	m := NewEmbeddingModel(p, "pplx-embed-v1-4b")
	result, err := m.DoEmbedMany(newCtx(), []string{"sunny day at the beach", "rainy day in the city"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany() error = %v", err)
	}
	if len(result.Embeddings) != 2 {
		t.Fatalf("embeddings len = %d, want 2", len(result.Embeddings))
	}
	for i, want := range dummy {
		if len(result.Embeddings[i]) != len(want) {
			t.Fatalf("embedding[%d] len = %d, want %d", i, len(result.Embeddings[i]), len(want))
		}
		for j, v := range want {
			if result.Embeddings[i][j] != float64(v) {
				t.Fatalf("embedding[%d][%d] = %v, want %v", i, j, result.Embeddings[i][j], v)
			}
		}
	}
	if result.Usage.Tokens != 8 || result.Usage.InputTokens != 8 {
		t.Fatalf("usage = %+v, want tokens=8", result.Usage)
	}
}

func TestPerplexityEmbeddingModelDecodesBinaryEmbeddings(t *testing.T) {
	raw := []byte{0, 128, 255, 64}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["encoding_format"] != "base64_binary" {
			t.Fatalf("encoding_format = %v, want base64_binary", body["encoding_format"])
		}
		if body["dimensions"] != float64(256) {
			t.Fatalf("dimensions = %v, want 256", body["dimensions"])
		}
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "index": 0, "embedding": encodeBinary(raw)},
			},
			"model": "pplx-embed-v1-4b",
			"usage": map[string]interface{}{"prompt_tokens": 3, "total_tokens": 3},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	m := NewEmbeddingModel(p, "pplx-embed-v1-4b")
	dims := 256
	result, err := m.DoEmbedMany(newCtx(), []string{"x"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{"dimensions": dims, "encodingFormat": "base64_binary"},
		},
	})
	if err != nil {
		t.Fatalf("DoEmbedMany() error = %v", err)
	}
	want := []float64{0, 128, 255, 64}
	if len(result.Embeddings[0]) != len(want) {
		t.Fatalf("embedding len = %d, want %d", len(result.Embeddings[0]), len(want))
	}
	for i, v := range want {
		if result.Embeddings[0][i] != v {
			t.Fatalf("embedding[%d] = %v, want %v", i, result.Embeddings[0][i], v)
		}
	}
}

func TestPerplexityEmbeddingModelCostProviderMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "index": 0, "embedding": encodeInt8([]int8{1, 2})},
			},
			"model": "pplx-embed-v1-4b",
			"usage": map[string]interface{}{
				"prompt_tokens": 8,
				"total_tokens":  8,
				"cost":          map[string]interface{}{"input_cost": 0.0001, "total_cost": 0.0001, "currency": "USD"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	m := NewEmbeddingModel(p, "pplx-embed-v1-4b")
	result, err := m.DoEmbedMany(newCtx(), []string{"x"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany() error = %v", err)
	}
	meta, ok := result.ProviderMetadata["perplexity"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata = %#v, want perplexity cost metadata", result.ProviderMetadata)
	}
	cost, ok := meta["cost"].(map[string]interface{})
	if !ok || cost["inputCost"] != 0.0001 || cost["totalCost"] != 0.0001 || cost["currency"] != "USD" {
		t.Fatalf("cost metadata = %#v, want inputCost/totalCost/currency", meta["cost"])
	}
}

func TestPerplexityEmbeddingModelOmitsMetadataWhenNoCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "index": 0, "embedding": encodeInt8([]int8{1, 2})},
			},
			"model": "pplx-embed-v1-4b",
			"usage": map[string]interface{}{"prompt_tokens": 8, "total_tokens": 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	m := NewEmbeddingModel(p, "pplx-embed-v1-4b")
	result, err := m.DoEmbedMany(newCtx(), []string{"x"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany() error = %v", err)
	}
	if result.ProviderMetadata != nil {
		t.Fatalf("providerMetadata = %#v, want nil when no cost returned", result.ProviderMetadata)
	}
}

func TestPerplexityEmbeddingModelTooManyValues(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewEmbeddingModel(p, "pplx-embed-v1-4b")
	values := make([]string, 513)
	for i := range values {
		values[i] = "v"
	}
	_, err := m.DoEmbedMany(newCtx(), values, nil)
	if err == nil {
		t.Fatal("DoEmbedMany() error = nil, want TooManyEmbeddingValuesForCallError")
	}
	if !providererrors.IsTooManyEmbeddingValuesForCallError(err) {
		t.Fatalf("error = %v (%T), want TooManyEmbeddingValuesForCallError", err, err)
	}
}

func TestPerplexityEmbeddingModelDoEmbedSingle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		inputs, _ := body["input"].([]interface{})
		if len(inputs) != 1 || inputs[0] != "hello" {
			t.Fatalf("request body input = %#v, want [hello]", body["input"])
		}
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "index": 0, "embedding": encodeInt8([]int8{1, 2, 3})},
			},
			"model": "pplx-embed-v1-4b",
			"usage": map[string]interface{}{"prompt_tokens": 2, "total_tokens": 2},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	prov, err := p.EmbeddingModel("pplx-embed-v1-4b")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	result, err := prov.DoEmbed(newCtx(), "hello", nil)
	if err != nil {
		t.Fatalf("DoEmbed() error = %v", err)
	}
	if len(result.Embedding) != 3 {
		t.Fatalf("embedding len = %d, want 3", len(result.Embedding))
	}
}

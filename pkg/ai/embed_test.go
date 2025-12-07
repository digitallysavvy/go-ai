package ai

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestEmbed_Basic(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	result, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "Hello, world!",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Embedding) == 0 {
		t.Error("expected non-empty embedding")
	}
}

func TestEmbed_NilModel(t *testing.T) {
	t.Parallel()

	_, err := Embed(context.Background(), EmbedOptions{
		Model: nil,
		Input: "Hello",
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if err.Error() != "model is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEmbed_EmptyInput(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "",
	})

	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if err.Error() != "input is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEmbed_Error(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("embedding failed")
	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(ctx context.Context, input string) (*types.EmbeddingResult, error) {
			return nil, expectedErr
		},
	}

	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "Hello",
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestEmbedMany_Basic(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	result, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:  model,
		Inputs: []string{"Hello", "World", "Test"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(result.Embeddings))
	}
}

func TestEmbedMany_NilModel(t *testing.T) {
	t.Parallel()

	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:  nil,
		Inputs: []string{"Hello"},
	})

	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if err.Error() != "model is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEmbedMany_EmptyInputs(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{}

	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:  model,
		Inputs: []string{},
	})

	if err == nil {
		t.Fatal("expected error for empty inputs")
	}
	if err.Error() != "at least one input is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEmbedMany_Error(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("batch embedding failed")
	model := &testutil.MockEmbeddingModel{
		DoEmbedManyFunc: func(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
			return nil, expectedErr
		},
	}

	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:  model,
		Inputs: []string{"Hello", "World"},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0, 3.0}

	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if math.Abs(sim-1.0) > 1e-10 {
		t.Errorf("expected similarity 1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 0.0}
	b := []float64{-1.0, 0.0}

	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if math.Abs(sim-(-1.0)) > 1e-10 {
		t.Errorf("expected similarity -1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 0.0}
	b := []float64{0.0, 1.0}

	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if math.Abs(sim) > 1e-10 {
		t.Errorf("expected similarity 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_DimensionMismatch(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0}

	_, err := CosineSimilarity(a, b)
	if err == nil {
		t.Fatal("expected error for dimension mismatch")
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	t.Parallel()

	a := []float64{0.0, 0.0, 0.0}
	b := []float64{1.0, 2.0, 3.0}

	_, err := CosineSimilarity(a, b)
	if err == nil {
		t.Fatal("expected error for zero vector")
	}
}

func TestEuclideanDistance_Identical(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0, 3.0}

	dist, err := EuclideanDistance(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dist != 0.0 {
		t.Errorf("expected distance 0.0, got %f", dist)
	}
}

func TestEuclideanDistance_Basic(t *testing.T) {
	t.Parallel()

	a := []float64{0.0, 0.0}
	b := []float64{3.0, 4.0}

	dist, err := EuclideanDistance(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if math.Abs(dist-5.0) > 1e-10 {
		t.Errorf("expected distance 5.0, got %f", dist)
	}
}

func TestEuclideanDistance_DimensionMismatch(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0}

	_, err := EuclideanDistance(a, b)
	if err == nil {
		t.Fatal("expected error for dimension mismatch")
	}
}

func TestDotProduct_Basic(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 2.0, 3.0}
	b := []float64{4.0, 5.0, 6.0}

	product, err := DotProduct(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1*4 + 2*5 + 3*6 = 4 + 10 + 18 = 32
	if product != 32.0 {
		t.Errorf("expected dot product 32.0, got %f", product)
	}
}

func TestDotProduct_DimensionMismatch(t *testing.T) {
	t.Parallel()

	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0}

	_, err := DotProduct(a, b)
	if err == nil {
		t.Fatal("expected error for dimension mismatch")
	}
}

func TestNormalize_Basic(t *testing.T) {
	t.Parallel()

	v := []float64{3.0, 4.0}
	normalized := Normalize(v)

	// Expected: [0.6, 0.8] (3/5, 4/5)
	if math.Abs(normalized[0]-0.6) > 1e-10 {
		t.Errorf("expected 0.6, got %f", normalized[0])
	}
	if math.Abs(normalized[1]-0.8) > 1e-10 {
		t.Errorf("expected 0.8, got %f", normalized[1])
	}

	// Check unit length
	var norm float64
	for _, x := range normalized {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 1e-10 {
		t.Errorf("expected unit length, got %f", norm)
	}
}

func TestNormalize_ZeroVector(t *testing.T) {
	t.Parallel()

	v := []float64{0.0, 0.0, 0.0}
	normalized := Normalize(v)

	// Zero vector should return unchanged
	for i, x := range normalized {
		if x != 0.0 {
			t.Errorf("expected 0.0 at index %d, got %f", i, x)
		}
	}
}

func TestFindMostSimilar_Basic(t *testing.T) {
	t.Parallel()

	query := []float64{1.0, 0.0}
	candidates := [][]float64{
		{0.0, 1.0},   // orthogonal
		{1.0, 0.0},   // identical
		{-1.0, 0.0},  // opposite
		{0.5, 0.5},   // partial
	}

	index, similarity, err := FindMostSimilar(query, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if index != 1 {
		t.Errorf("expected index 1 (identical), got %d", index)
	}
	if math.Abs(similarity-1.0) > 1e-10 {
		t.Errorf("expected similarity 1.0, got %f", similarity)
	}
}

func TestFindMostSimilar_EmptyCandidates(t *testing.T) {
	t.Parallel()

	query := []float64{1.0, 0.0}
	candidates := [][]float64{}

	_, _, err := FindMostSimilar(query, candidates)
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
}

func TestRankBySimilarity_Basic(t *testing.T) {
	t.Parallel()

	query := []float64{1.0, 0.0}
	candidates := [][]float64{
		{0.0, 1.0},   // orthogonal - 0.0
		{1.0, 0.0},   // identical - 1.0
		{-1.0, 0.0},  // opposite - -1.0
	}

	indices, similarities, err := RankBySimilarity(query, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(indices) != 3 {
		t.Fatalf("expected 3 results, got %d", len(indices))
	}

	// Should be sorted by similarity descending: 1 (1.0), 0 (0.0), 2 (-1.0)
	if indices[0] != 1 {
		t.Errorf("expected first index to be 1, got %d", indices[0])
	}
	if indices[1] != 0 {
		t.Errorf("expected second index to be 0, got %d", indices[1])
	}
	if indices[2] != 2 {
		t.Errorf("expected third index to be 2, got %d", indices[2])
	}

	if math.Abs(similarities[0]-1.0) > 1e-10 {
		t.Errorf("expected first similarity 1.0, got %f", similarities[0])
	}
}

func TestRankBySimilarity_EmptyCandidates(t *testing.T) {
	t.Parallel()

	query := []float64{1.0, 0.0}
	candidates := [][]float64{}

	indices, similarities, err := RankBySimilarity(query, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(indices) != 0 {
		t.Errorf("expected empty indices, got %d", len(indices))
	}
	if len(similarities) != 0 {
		t.Errorf("expected empty similarities, got %d", len(similarities))
	}
}

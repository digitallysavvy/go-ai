package ai

import (
	"errors"
	"math"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Replays packages/ai/src/util/cosine-similarity.test.ts and
// generate-text/sum-token-counts.test.ts case for case.

func TestTSReplay_CosineSimilarity(t *testing.T) {
	close5 := func(got, want float64) bool { return math.Abs(got-want) < 0.5e-5 }

	if got, err := CosineSimilarity([]float64{1, 2, 3}, []float64{4, 5, 6}); err != nil || !close5(got, 0.9746318461970762) {
		t.Fatalf("positive: %v, %v", got, err)
	}
	if got, err := CosineSimilarity([]float64{1, 0}, []float64{-1, 0}); err != nil || !close5(got, -1) {
		t.Fatalf("negative: %v, %v", got, err)
	}
	_, err := CosineSimilarity([]float64{1, 2, 3}, []float64{4, 5})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("different lengths: error = %v, want InvalidArgumentError", err)
	}
	zero := []float64{0, 0, 0}
	v := []float64{0, 1, 2}
	if got, _ := CosineSimilarity(v, zero); got != 0 {
		t.Fatalf("zero vector (second) = %v", got)
	}
	if got, _ := CosineSimilarity(zero, v); got != 0 {
		t.Fatalf("zero vector (first) = %v", got)
	}
	if got, _ := CosineSimilarity([]float64{1e-10, 0, 0}, []float64{2e-10, 0, 0}); got != 1 {
		t.Fatalf("small magnitudes = %v, want exactly 1", got)
	}
	if got, _ := CosineSimilarity([]float64{1e-10, 0, 0}, []float64{-1e-10, 0, 0}); got != -1 {
		t.Fatalf("small opposite magnitudes = %v, want exactly -1", got)
	}
}

func TestTSReplay_SumTokenCounts(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	if got := sumTokenCounts(n(3), n(10)); got == nil || *got != 13 {
		t.Fatalf("3+10 = %v", got)
	}
	if got := sumTokenCounts(nil, n(10)); got == nil || *got != 10 {
		t.Fatalf("undefined+10 = %v", got)
	}
	if got := sumTokenCounts(n(3), nil); got == nil || *got != 3 {
		t.Fatalf("3+undefined = %v", got)
	}
	if got := sumTokenCounts(nil, nil); got != nil {
		t.Fatalf("undefined+undefined = %v, want nil", *got)
	}
}

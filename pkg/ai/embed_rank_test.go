package ai

import (
	"reflect"
	"testing"
)

func TestRankBySimilarity_TiesKeepInputOrder(t *testing.T) {
	q := []float64{1, 0}
	cands := [][]float64{{0, 1}, {1, 0}, {2, 0}, {0, 1}, {3, 0}}
	idx, _, err := RankBySimilarity(q, cands)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 4, 0, 3}; !reflect.DeepEqual(idx, want) {
		t.Fatalf("idx = %v, want %v", idx, want)
	}
}

func BenchmarkRankBySimilarity(b *testing.B) {
	cands := make([][]float64, 20000)
	for i := range cands {
		cands[i] = []float64{float64(i%97) + 1, 1}
	}
	for i := 0; i < b.N; i++ {
		_, _, _ = RankBySimilarity([]float64{1, 1}, cands)
	}
}

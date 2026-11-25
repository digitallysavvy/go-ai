package ai

import "testing"

// TS util/cosine-similarity.ts: InvalidArgumentError({parameter:
// 'vector1,vector2', message: 'Vectors must have the same length'}), whose
// message is "Invalid argument for parameter vector1,vector2: Vectors must
// have the same length" (the lengths are the error's value).
func TestR89carryCosineSimilarityLengthError(t *testing.T) {
	_, err := CosineSimilarity([]float64{1}, []float64{1, 2})
	want := "Invalid argument for parameter vector1,vector2: Vectors must have the same length"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %s", err, want)
	}
}

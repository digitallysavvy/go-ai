package cohere

import "testing"

// Round 96 integrator sweep (ruling 4): the hand-written cohere chunk
// schema's z.number() checks reject a non-finite number (JSON.parse of
// 1e400), as zod does.
func TestR96integChunkNumberRejectsNonFinite(t *testing.T) {
	chunk := func(index string) []byte {
		return []byte(`{"type":"content-delta","index":` + index + `,"delta":{"message":{"content":{"text":"a"}}}}`)
	}
	if err := validateCohereChunk(chunk("0")); err != nil {
		t.Fatalf("finite index: err = %v", err)
	}
	if err := validateCohereChunk(chunk("1e400")); err == nil {
		t.Fatal("1e400 index: err = nil, want the schema failure")
	}
}

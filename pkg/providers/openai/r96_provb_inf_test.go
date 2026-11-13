package openai

import (
	"math"
	"testing"
)

// TestR96provbInfMetadataNull: provider metadata holding a non-finite
// provider number serializes it as null (jsonstr, JSON.stringify) instead of
// dropping the whole metadata (main ruling 4, write side).
func TestR96provbInfMetadataNull(t *testing.T) {
	raw := toRawMetadata(map[string]interface{}{"openai": map[string]interface{}{"x": math.Inf(1), "y": math.NaN()}})
	if string(raw) != `{"openai":{"x":null,"y":null}}` {
		t.Fatalf("metadata = %s", raw)
	}
	if got := mustMarshalCompletionProviderMetadata(map[string]interface{}{"openai": map[string]interface{}{"x": math.Inf(-1)}}); string(got) != `{"openai":{"x":null}}` {
		t.Fatalf("completion metadata = %s", got)
	}
}

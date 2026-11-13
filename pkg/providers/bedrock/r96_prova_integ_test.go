package bedrock

import (
	"math"
	"testing"
)

// Ruling 4 (r96): provider metadata holding a non-finite number is written
// with null (JSON.stringify), not dropped.
func TestR96provaProviderMetadataNonFinite(t *testing.T) {
	if got := string(mustMarshalProviderMetadata(map[string]interface{}{"x": math.Inf(-1)})); got != `{"x":null}` {
		t.Fatalf("metadata = %q", got)
	}
}

package agent

import (
	"math"
	"testing"
)

// Ruling 4 (round 96): provider metadata holding a non-finite number keeps
// the metadata with the value as null (TS JSON.stringify), instead of
// dropping it.
func TestR96genProviderMetadataRawNonFinite(t *testing.T) {
	raw := agentProviderMetadataRaw(map[string]interface{}{"p": map[string]interface{}{"x": math.Inf(-1)}})
	if string(raw) != `{"p":{"x":null}}` {
		t.Fatalf("raw = %q, want {\"p\":{\"x\":null}}", raw)
	}
}

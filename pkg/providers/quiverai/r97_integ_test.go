package quiverai

import (
	"encoding/json"
	"testing"
)

// TestR97integReplayMetadataKeepsPrototypeKey: TS reads
// part.providerMetadata?.quiverai from the in-memory prompt object (no JSON
// parse), so a __proto__ key next to itemId must not drop the metadata.
func TestR97integReplayMetadataKeepsPrototypeKey(t *testing.T) {
	got := decodeQuiverAIProviderMetadata(json.RawMessage(`{"quiverai":{"__proto__":{"a":1},"itemId":"it_1"}}`))
	if got == nil || got["itemId"] != "it_1" {
		t.Fatalf("metadata = %v", got)
	}
}

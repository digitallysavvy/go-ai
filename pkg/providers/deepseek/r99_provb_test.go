package deepseek

import (
	"encoding/json"
	"testing"
)

// TestR99provbPartOptionMetadataOutOfRange: TS reads part provider metadata
// as an object, a 1e400 sibling being Infinity (ruling 4). Go decoded it with
// encoding/json, which fails on 1e400 and dropped the whole metadata.
func TestR99provbPartOptionMetadataOutOfRange(t *testing.T) {
	meta := json.RawMessage(`{"deepseek":{"itemId":"r1","n":1e400}}`)
	if got := deepseekResponsesPartOption(nil, meta, "deepseek", "itemId"); got != "r1" {
		t.Errorf("itemId = %v, want r1", got)
	}
}

package openresponses

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestR99provbExtensionMetadataOutOfRange: TS holds provider metadata as an
// object (a 1e400 value reads as Infinity, ruling 4). Go decoded it with
// encoding/json, which fails on 1e400, so the existing metadata was lost.
func TestR99provbExtensionMetadataOutOfRange(t *testing.T) {
	raw := json.RawMessage(`{"other":{"keep":true,"n":1e400}}`)
	if got := extensionToolCallProviderMetadata(raw); got == nil || got["other"] == nil {
		t.Errorf("metadata = %v, want the other namespace kept", got)
	}
	merged := mergeExtensionReference(raw, "p", map[string]interface{}{"id": "x"})
	if !strings.Contains(string(merged), `"keep":true`) || !strings.Contains(string(merged), `"openResponsesExtension"`) {
		t.Errorf("merged = %s, want the existing metadata kept", merged)
	}
}

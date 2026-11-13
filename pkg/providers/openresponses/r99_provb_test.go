package openresponses

import (
	"encoding/json"
	"math"
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

// TestR99provbMessageItemNonFiniteContent: JSON.stringify writes a
// non-finite number as null (ruling 4). jsonstr could not null one reached
// through the unexported embedded alias, so an explicit empty id with such
// content failed to marshal.
func TestR99provbMessageItemNonFiniteContent(t *testing.T) {
	item := MessageItem{Type: "message", Role: "user", Content: []interface{}{map[string]interface{}{"n": math.Inf(1)}}, idSet: true}
	b, err := json.Marshal(item)
	if err != nil || string(b) != `{"type":"message","role":"user","content":[{"n":null}],"id":""}` {
		t.Errorf("marshal = %s, %v", b, err)
	}
}

// TestR99provbProviderDataOutOfRange: TS getProviderData reads part
// provider metadata as an object (a 1e400 value reads as Infinity, ruling
// 4). Go's nested encoding/json decode failed on 1e400 and lost the item id.
func TestR99provbProviderDataOutOfRange(t *testing.T) {
	meta := json.RawMessage(`{"openai":{"itemId":"msg_1","n":1e400}}`)
	if got := openResponsesProviderData(nil, meta, "openai"); got == nil || got["itemId"] != "msg_1" {
		t.Errorf("provider data = %v, want itemId msg_1", got)
	}
}

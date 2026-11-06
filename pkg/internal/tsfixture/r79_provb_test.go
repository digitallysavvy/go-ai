package tsfixture

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Call-level providerOptions and headers drop TS `undefined` values like the
// prompt and tool provider options do (TS option schemas read an undefined
// property as absent; combineHeaders drops undefined headers).
func TestR79provbBuildOptionsDropsUndefined(t *testing.T) {
	undef := map[string]interface{}{"__undef": true}
	o, why := BuildOptions(map[string]interface{}{
		"prompt":          []interface{}{},
		"providerOptions": map[string]interface{}{"openai": map[string]interface{}{"a": undef, "b": 1.0}},
		"headers":         map[string]interface{}{"x-a": undef, "x-b": "v"},
	})
	if why != "" {
		t.Fatalf("unsupported: %s", why)
	}
	po, _ := o.ProviderOptions["openai"].(map[string]interface{})
	if _, ok := po["a"]; ok || po["b"] != 1.0 {
		t.Fatalf("providerOptions = %#v", o.ProviderOptions)
	}
	if _, ok := o.Headers["x-a"]; ok || o.Headers["x-b"] != "v" {
		t.Fatalf("headers = %#v", o.Headers)
	}
}

// prova CR r79 item 1: a TS Buffer.from(...) extracts as Buffer.toJSON
// ({type: "Buffer", data: [...]}); WireBytes reads it as bytes, so prompt file
// parts built from it replay instead of being skipped.
func TestR79provbWireBytesNodeBuffer(t *testing.T) {
	buf := map[string]interface{}{"type": "Buffer", "data": []interface{}{84.0, 104.0, 0.0, 255.0}}
	b, ok := WireBytes(buf)
	if !ok || string(b) != "Th\x00\xff" {
		t.Fatalf("WireBytes = %q, %v", b, ok)
	}
	for _, bad := range []map[string]interface{}{
		{"type": "Buffer", "data": []interface{}{256.0}},
		{"type": "Buffer", "data": []interface{}{1.5}},
		{"type": "Buffer", "data": []interface{}{1.0}, "extra": true},
		{"type": "data", "data": []interface{}{1.0}},
	} {
		if _, ok := WireBytes(bad); ok {
			t.Fatalf("WireBytes(%v) accepted", bad)
		}
	}
	msgs, why := BuildPromptMessages([]interface{}{map[string]interface{}{
		"role": "user",
		"content": []interface{}{map[string]interface{}{
			"type": "file", "mediaType": "image/png",
			"data": map[string]interface{}{"type": "data", "data": buf},
		}},
	}})
	if why != "" {
		t.Fatalf("unsupported: %s", why)
	}
	fc, ok := msgs[0].Content[0].(types.FileContent)
	if !ok || string(fc.FileData.Data) != "Th\x00\xff" {
		t.Fatalf("file part = %#v", msgs[0].Content[0])
	}
}

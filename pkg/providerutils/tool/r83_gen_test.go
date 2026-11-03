package tool

import "testing"

// TS anthropic sanitize-json-schema.ts: schema.items != null, so a JSON null
// items is left out (prova CR r83 item 1).
func TestR83genSanitizeAnthropicNullItems(t *testing.T) {
	out, ok := SanitizeAnthropicSchema(map[string]interface{}{"type": "array", "items": nil}).(map[string]interface{})
	if !ok {
		t.Fatal("not a map")
	}
	if _, has := out["items"]; has {
		t.Fatalf("items kept: %v", out)
	}
}

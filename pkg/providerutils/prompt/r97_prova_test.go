package prompt

import "testing"

// TS validateTypes runs the provider tool output schema on the caller's value
// itself: zod strips an unknown __proto__ key, so the value passes (the
// secureJsonParse rejection applies to response text, not to tool outputs).
func TestR97provaToolOutputWithPrototypeKeyValidates(t *testing.T) {
	value := []interface{}{map[string]interface{}{
		"type": "web_search_result", "url": "https://example.com", "title": nil,
		"pageAge": nil, "encryptedContent": "enc", "__proto__": map[string]interface{}{},
	}}
	if _, err := validateAnthropicToolOutput(value, zWebSearchOutput); err != nil {
		t.Fatalf("validate = %v, want the value accepted", err)
	}
	if _, err := validateAnthropicToolOutput([]interface{}{map[string]interface{}{"type": "web_search_result"}}, zWebSearchOutput); err == nil {
		t.Fatal("an output missing url still validated")
	}
}

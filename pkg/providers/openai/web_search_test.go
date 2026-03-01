package openai

import (
	"encoding/json"
	"testing"
)

// BUG-T10: the web search call action field is optional — the API may omit it (#12706).
// Verifies that WebSearchCallItem with no "action" key in the JSON unmarshals without error
// and leaves Action as nil.
func TestWebSearchCallItem_ActionOptional(t *testing.T) {
	// JSON that deliberately omits the "action" field.
	raw := `{"type":"web_search_call","id":"ws_123","status":"completed"}`

	var item WebSearchCallItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if item.Type != "web_search_call" {
		t.Errorf("Type = %q, want %q", item.Type, "web_search_call")
	}
	if item.Status != "completed" {
		t.Errorf("Status = %q, want %q", item.Status, "completed")
	}
	if item.Action != nil {
		t.Errorf("Action should be nil when omitted, got %+v", item.Action)
	}
}

// TestWebSearchCallItem_ActionPresent verifies that when the "action" field is present
// it is parsed correctly.
func TestWebSearchCallItem_ActionPresent(t *testing.T) {
	query := "golang generics"
	raw := `{"type":"web_search_call","id":"ws_456","status":"completed","action":{"type":"search","query":"golang generics"}}`

	var item WebSearchCallItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if item.Action == nil {
		t.Fatal("Action should not be nil when present")
	}
	if item.Action.Type != "search" {
		t.Errorf("Action.Type = %q, want %q", item.Action.Type, "search")
	}
	if item.Action.Query == nil || *item.Action.Query != query {
		t.Errorf("Action.Query = %v, want %q", item.Action.Query, query)
	}
}

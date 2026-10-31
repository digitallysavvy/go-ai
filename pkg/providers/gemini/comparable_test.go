package gemini

import (
	"encoding/json"
	"testing"
)

// v0.5.2 callers compare Part values with ==; a decoded toolCall must not
// make the struct incomparable.
func TestPartStaysComparable(t *testing.T) {
	var a, b Part
	if err := json.Unmarshal([]byte(`{"toolCall":{"toolType":"x","args":{"b":1,"a":2},"id":"1"}}`), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"toolCall":{"toolType":"x","args":{"b":1,"a":2},"id":"1"}}`), &b); err != nil {
		t.Fatal(err)
	}
	if a.toolCallArgsRaw == "" {
		t.Fatal("raw args not kept")
	}
	_ = a == Part{} // compile-time: Part is comparable
}

package types

import (
	"encoding/json"
	"testing"
)

func TestToolCallContentAlwaysIncludesInput(t *testing.T) {
	b, err := json.Marshal(ToolCallContent{ToolCallID: "1", ToolName: "t"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	in, ok := m["input"].(map[string]any)
	if !ok || len(in) != 0 {
		t.Fatalf("expected input {}, got %s", b)
	}
}

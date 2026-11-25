package openresponses

import (
	"encoding/json"
	"testing"
)

// The lenient response decode (openResponsesLenientUnmarshal) reads keys by
// their exact names, as TS reads the body's properties: a key that differs
// only in case fills nothing, also inside the output items and the usage
// (review round 95, guard extension to interface{} decode helpers).
func TestR95integLenientDecodeKeysMatchExactly(t *testing.T) {
	var w openResponsesResponseWire
	body := `{"id":"r","Model":"m","model":"x","output":[{"type":"message","ID":"i","Role":"assistant"}],"usage":{"Input_Tokens":5,"output_tokens":2}}`
	if err := json.Unmarshal([]byte(body), &w); err != nil {
		t.Fatal(err)
	}
	if w.Model != "x" || len(w.Output) != 1 || w.Output[0].ID != "" || w.Output[0].Role != "" ||
		w.Usage == nil || w.Usage.InputTokens != 0 || w.Usage.OutputTokens != 2 {
		t.Fatalf("decoded %+v (usage %+v), want only the exact keys", w.OpenResponsesResponse, w.Usage)
	}
	// A mistyped field still keeps its zero value without failing (lenient).
	var w2 openResponsesResponseWire
	if err := json.Unmarshal([]byte(`{"id":"r","model":5,"Model":"m"}`), &w2); err != nil || w2.Model != "" || w2.ID != "r" {
		t.Fatalf("err = %v, decoded %+v", err, w2.OpenResponsesResponse)
	}
}

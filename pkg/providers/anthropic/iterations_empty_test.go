package anthropic

import (
	"encoding/json"
	"testing"
)

// TS maps `iterations: []` to `[]` (an empty array is truthy) and only a
// missing/null value to null.
func TestIterationsEmptyArrayStaysEmpty(t *testing.T) {
	var empty, missing anthropicUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":1,"output_tokens":2,"iterations":[]}`), &empty); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"input_tokens":1,"output_tokens":2}`), &missing); err != nil {
		t.Fatal(err)
	}
	got := anthropicProviderMetadata(empty, "", nil, nil, nil)["anthropic"].(map[string]interface{})["iterations"]
	b, _ := json.Marshal(got)
	if string(b) != "[]" {
		t.Fatalf("empty iterations metadata = %s, want []", b)
	}
	if got := anthropicProviderMetadata(missing, "", nil, nil, nil)["anthropic"].(map[string]interface{})["iterations"]; got != nil {
		t.Fatalf("missing iterations metadata = %#v, want nil", got)
	}
	raw := anthropicRawUsage(empty)
	b, _ = json.Marshal(raw["iterations"])
	if string(b) != "[]" {
		t.Fatalf("raw usage iterations = %s, want []", b)
	}
}

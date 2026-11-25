package openresponses

import (
	"encoding/json"
	"testing"
)

// provg r90 low class: the lenient Open Responses decoder left a fractional
// count at zero; it is truncated into the int field now (TS keeps the
// number; S1 for the integer Usage fields).
func TestR90integFractionalUsageNotZeroed(t *testing.T) {
	var u Usage
	if err := json.Unmarshal([]byte(`{"input_tokens":2.5,"output_tokens":3.5,"total_tokens":6}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != 2 || u.OutputTokens != 3 || u.TotalTokens != 6 {
		t.Fatalf("usage = %+v, want 2 / 3 / 6", u)
	}
}

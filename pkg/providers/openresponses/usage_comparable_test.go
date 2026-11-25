package openresponses

import (
	"encoding/json"
	"testing"
)

// v0.5.2 callers compare Usage values with ==; the kept wire object must not
// make the struct incomparable, and must still round-trip into Raw.
func TestUsageStaysComparableAndKeepsWire(t *testing.T) {
	var u Usage
	if err := json.Unmarshal([]byte(`{"input_tokens":1,"output_tokens":2,"total_tokens":3,"extra":true}`), &u); err != nil {
		t.Fatal(err)
	}
	if u == (Usage{}) {
		t.Fatal("decoded usage equals zero value")
	}
	if w := u.wire(); w == nil || w["extra"] != true {
		t.Fatalf("wire = %v", w)
	}
}

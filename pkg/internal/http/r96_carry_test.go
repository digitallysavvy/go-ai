package http

import (
	"math"
	"testing"
)

// A JSON response body holding a number beyond the double range under a
// free-form value decodes, as JSON.parse reads it (+Inf).
func TestR96carryDecodeJSONBodyOutOfRange(t *testing.T) {
	var got struct {
		ID       string                 `json:"id"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := decodeJSONBody([]byte(`{"id":"a","metadata":{"v":1e400}}`), &got); err != nil {
		t.Fatalf("decodeJSONBody: %v", err)
	}
	if f, _ := got.Metadata["v"].(float64); got.ID != "a" || !math.IsInf(f, 1) {
		t.Errorf("got %#v", got)
	}
}

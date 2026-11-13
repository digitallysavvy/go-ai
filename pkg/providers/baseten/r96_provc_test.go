package baseten

import (
	"math"
	"testing"
)

// TestR96provcErrorSchemaNonFiniteCode: basetenErrorSchema's code is
// z.union([z.string(), z.number()]).nullish(), and z.number() rejects
// Infinity (JSON.parse of 1e400), so such a body does not match the schema.
func TestR96provcErrorSchemaNonFiniteCode(t *testing.T) {
	for _, code := range []interface{}{math.Inf(1), math.NaN()} {
		if _, _, ok := basetenErrorSchema(map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": code}}); ok {
			t.Errorf("code %v: matched, want the schema failure", code)
		}
	}
	if msg, _, ok := basetenErrorSchema(map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": 5.0}}); !ok || msg != "m" {
		t.Errorf("finite code: msg=%q ok=%v", msg, ok)
	}
}

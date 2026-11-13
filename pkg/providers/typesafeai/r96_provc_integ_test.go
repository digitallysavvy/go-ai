package typesafeai

import "testing"

// TestR96provcResponseOutOfRangeNumber: an undeclared key holding a number
// beyond the double range passes the response schema (JSON.parse reads
// Infinity, zod strips the key); a declared z.number() holding it fails
// (zod rejects Infinity).
func TestR96provcResponseOutOfRangeNumber(t *testing.T) {
	if err := validateDecisionResponse(200, nil, []byte(`{"answers":{},"extra":1e400}`)); err != nil {
		t.Fatalf("undeclared key: err = %v", err)
	}
	if err := validateDecisionResponse(200, nil, []byte(`{"answers":{"q":{"type":"noul","noul":1e400}}}`)); err == nil {
		t.Fatal("declared number: err = nil, want the schema failure")
	}
}

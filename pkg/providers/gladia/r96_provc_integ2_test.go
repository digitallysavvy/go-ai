package gladia

import "testing"

// TestR96provcErrorCodeOutOfRange: error.code is z.number(), which rejects
// Infinity (JSON.parse of 1e400), so the body does not match the error
// schema and the message is the HTTP status text.
func TestR96provcErrorCodeOutOfRange(t *testing.T) {
	if msg, code := parseGladiaErrorBody(500, []byte(`{"error":{"message":"m","code":1e400}}`)); msg != "Internal Server Error" || code != "" {
		t.Fatalf("msg=%q code=%q", msg, code)
	}
}

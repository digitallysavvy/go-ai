package hume

import "testing"

// TestR96provcErrorCodeOutOfRange: error.code is z.number(), which rejects
// Infinity (JSON.parse of 1e400): status text, no data.
func TestR96provcErrorCodeOutOfRange(t *testing.T) {
	body := []byte(`{"error":{"message":"m","code":1e400}}`)
	if msg, code := errorMessageAndCode(500, body); msg != "Internal Server Error" || code != "" {
		t.Fatalf("msg=%q code=%q", msg, code)
	}
	if data := humeErrorData(body); data != nil {
		t.Fatalf("data = %#v, want nil", data)
	}
}

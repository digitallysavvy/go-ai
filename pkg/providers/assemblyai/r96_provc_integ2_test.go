package assemblyai

import "testing"

// TestR96provcErrorDataCodeOutOfRange: assemblyaiErrorDataSchema's nested
// code is z.number(), which rejects Infinity (JSON.parse of 1e400): no data.
func TestR96provcErrorDataCodeOutOfRange(t *testing.T) {
	if data := assemblyAIErrorData([]byte(`{"error":{"message":"m","code":1e400}}`)); data != nil {
		t.Fatalf("data = %#v, want nil", data)
	}
}

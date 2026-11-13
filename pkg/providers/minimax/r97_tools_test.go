package minimax

import "testing"

// TS minimaxVideoErrorSchema http_code is z.union([z.string(), z.number()]):
// a non-finite number (JSON.parse of 1e400) fails the schema, so the error
// message is the HTTP status text with no data (ruling 4, review round 97).
func TestR97toolsVideoErrorNonFiniteHTTPCode(t *testing.T) {
	m := &VideoModel{modelID: ModelH3}
	err := m.parseAPIError([]byte(`{"error":{"message":"boom","http_code":1e400}}`), 500, nil).(*Error)
	if err.Message != "Internal Server Error" || err.Data != nil {
		t.Fatalf("message = %q, data = %#v", err.Message, err.Data)
	}
	err = m.parseAPIError([]byte(`{"error":{"message":"boom","http_code":500}}`), 500, nil).(*Error)
	if err.Message != "boom" {
		t.Fatalf("finite code message = %q", err.Message)
	}
}

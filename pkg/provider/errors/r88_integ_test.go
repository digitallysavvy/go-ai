package errors

import "testing"

// prova r88 open item, class sweep (TS safeParseJSON): the generic error
// message extraction ignores a body with a __proto__ key.
func TestR88integExtractAPIErrorMessageSecureParse(t *testing.T) {
	if got := ExtractAPIErrorMessage([]byte(`{"error":{"message":"m"}}`)); got != "m" {
		t.Fatalf("clean body: %q", got)
	}
	if got := ExtractAPIErrorMessage([]byte(`{"error":{"message":"m"},"__proto__":{"x":1}}`)); got != "" {
		t.Fatalf("__proto__ body: %q", got)
	}
}

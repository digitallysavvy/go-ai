package providerutils

import (
	"net/http"
	"testing"
)

// zod matches object keys exactly, and JSON.parse reads an out-of-range
// number in another field as Infinity (stripped by the object schema), so:
// differently cased keys fall back to the status text, and an unrelated
// 1e400 does not.
func TestR94carryNestedErrorMessageExactKeys(t *testing.T) {
	for _, body := range []string{
		`{"Error":{"message":"m","code":1}}`,
		`{"error":{"Message":"m","code":1}}`,
		`{"error":{"message":"m","CODE":1}}`,
		`{"error":{"message":"m","code":1e400}}`,
	} {
		if msg, code := NestedErrorMessage(http.StatusBadRequest, []byte(body)); msg != "Bad Request" || code != "" {
			t.Errorf("%s: got %q / %q, want the status text", body, msg, code)
		}
	}
	if msg, code := NestedErrorMessage(http.StatusBadRequest, []byte(`{"error":{"message":"m","code":429,"x":1e400},"y":-1e400}`)); msg != "m" || code != "429" {
		t.Errorf("got %q / %q, want m / 429", msg, code)
	}
}

package errors

import (
	"errors"
	"testing"
)

// core2-21 (TS normalize-stream-provider-error.ts:30, getString): a record
// without a string message is returned unchanged, and an empty type wins
// over the outer one.
func TestR91integNormalizeStreamProviderErrorMessageAndType(t *testing.T) {
	raw := errors.New("x")
	if got := NormalizeStreamProviderError(raw, "p", map[string]interface{}{"error": map[string]interface{}{"code": 1}}); got != raw {
		t.Fatalf("no string message: got %#v, want the error unchanged", got)
	}
	got := NormalizeStreamProviderError(raw, "p", map[string]interface{}{"type": "t", "error": map[string]interface{}{"type": "", "message": "m"}})
	var spe *StreamProviderError
	if !errors.As(got, &spe) || spe.Type != "" || spe.Message != "m" {
		t.Fatalf("got %#v", got)
	}
}

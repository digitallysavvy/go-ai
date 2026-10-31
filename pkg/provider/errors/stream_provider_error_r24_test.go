package errors

import (
	stderrors "errors"
	"testing"
)

// A response object without its own error must fall back to outer.error
// (TS asRecord(response?.error) ?? asRecord(error) ?? outer).
func TestNormalizeStreamProviderErrorResponseWithoutErrorFallsBackToOuterError(t *testing.T) {
	data := map[string]interface{}{
		"response": map[string]interface{}{"id": "r1"},
		"error":    map[string]interface{}{"message": "boom", "type": "server_error", "code": "x"},
	}
	err := NormalizeStreamProviderError(stderrors.New("generic"), "p", data)
	spe, ok := err.(*StreamProviderError)
	if !ok {
		t.Fatalf("got %T", err)
	}
	if spe.Message != "boom" || spe.Type != "server_error" {
		t.Fatalf("outer.error not used: %+v", spe)
	}
}

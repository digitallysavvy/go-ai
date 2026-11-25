package errors

import (
	"errors"
	"testing"
)

// TS normalize-stream-provider-error.ts: typeof details.message === 'string'
// is enough, so an empty message is the message (provb CR).
func TestR87integNormalizeStreamProviderErrorEmptyMessage(t *testing.T) {
	err := NormalizeStreamProviderError(errors.New("raw"), "p", map[string]interface{}{
		"error": map[string]interface{}{"message": "", "type": "server_error"},
	})
	var se *StreamProviderError
	if !errors.As(err, &se) || se.Message != "" || se.Type != "server_error" {
		t.Fatalf("got %#v", err)
	}
}

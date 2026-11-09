package xai

import "testing"

// TestR90provbXAIErrorCodeType: an {error: {message, code}} body whose code
// is neither a string nor a number matches no member of TS
// xaiErrorDataSchema (xai-error.ts), so there is no message.
func TestR90provbXAIErrorCodeType(t *testing.T) {
	if msg, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":true}}`)); ok {
		t.Fatalf("matched with %q", msg)
	}
	if msg, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":7}}`)); !ok || msg != "m" {
		t.Fatalf("numeric code: %q %v", msg, ok)
	}
}

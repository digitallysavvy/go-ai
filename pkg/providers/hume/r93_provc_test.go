package hume

import "testing"

// TestR93provcNumericErrorCodeIsJSNumberText (cr-r93-provb-to-provc-carry):
// the Go-only ErrorCode of a numeric code reads like JS String(code).
func TestR93provcNumericErrorCodeIsJSNumberText(t *testing.T) {
	if _, code := errorMessageAndCode(400, []byte(`{"error":{"message":"m","code":1e21}}`)); code != "1e+21" {
		t.Errorf("code = %q, want 1e+21", code)
	}
}

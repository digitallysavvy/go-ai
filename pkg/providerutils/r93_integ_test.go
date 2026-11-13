package providerutils

import "testing"

// TestR93integNestedErrorCodeIsJSNumberText (cr-r93-provb-to-provc-carry):
// the Go-only error code of the shared {error:{message, code}} schema is
// the JS number text of code, as TS String(code) gives and as the provb and
// provc error mappers now report it.
func TestR93integNestedErrorCodeIsJSNumberText(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"m","code":1e21}}`:  "1e+21",
		`{"error":{"message":"m","code":1e-7}}`:  "1e-7",
		`{"error":{"message":"m","code":400}}`:   "400",
		`{"error":{"message":"m","code":-0.5}}`:  "-0.5",
		`{"error":{"message":"m","code":1.5e3}}`: "1500",
	} {
		msg, code := NestedErrorMessage(400, []byte(body))
		if msg != "m" || code != want {
			t.Errorf("%s: got (%q, %q), want (\"m\", %q)", body, msg, code, want)
		}
	}
}

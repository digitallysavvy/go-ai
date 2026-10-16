package bfl

import "testing"

// JSON.stringify(detail) normalizes numbers and escapes; it is not a byte-level compact.
func TestR33_DetailIsJSStringified(t *testing.T) {
	err := bflStatusError(422, nil, []byte(`{"detail":{"a":1.0,"b":1e2,"c":"é","a":2}}`))
	if got, want := err.Error(), `{"a":2,"b":100,"c":"é"}`; !contains(got, want) {
		t.Fatalf("got %q want contains %q", got, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

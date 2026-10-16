package fx

import "testing"

// TS JSON.stringify keeps insertion order and does not HTML-escape.
func TestCredentialFileContentKeyOrder(t *testing.T) {
	got := credentialFileContent("a<b", "r", 9007199254740991, "acct")
	want := `{"version":1,"access_token":"a<b","refresh_token":"r","expires_at_ms":9007199254740991,"account_id":"acct"}` + "\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TS readCredential requires Number.isSafeInteger(expires_at_ms).
func TestReadCredentialRequiresSafeInteger(t *testing.T) {
	base := func(v any) map[string]any {
		return map[string]any{"version": float64(1), "access_token": "a", "refresh_token": "r", "expires_at_ms": v, "account_id": "x"}
	}
	for _, v := range []float64{1.5, 9007199254740992, 1e300} {
		if readCredential(base(v)) != nil {
			t.Errorf("expires_at_ms %v accepted", v)
		}
	}
	if readCredential(base(float64(1700000000000))) == nil {
		t.Error("safe integer rejected")
	}
}

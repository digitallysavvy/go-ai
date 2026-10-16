package cursor

import "testing"

func TestParseAccessTokenRejectsForbiddenPrototypeKey(t *testing.T) {
	if tok, ok := parseAccessToken([]byte(`{"accessToken":"t"}`)); !ok || tok != "t" {
		t.Fatal("control must parse")
	}
	if _, ok := parseAccessToken([]byte(`{"accessToken":"t","__proto__":{}}`)); ok {
		t.Fatal("__proto__ must be rejected")
	}
}

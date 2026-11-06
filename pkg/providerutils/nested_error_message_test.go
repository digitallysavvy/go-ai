package providerutils

import "testing"

func TestNestedErrorMessage(t *testing.T) {
	for _, c := range []struct {
		name, body, wantMsg, wantCode string
		status                        int
	}{
		{"match", `{"error":{"message":"bad","code":12}}`, "bad", "12", 400},
		{"empty message kept", `{"error":{"message":"","code":1}}`, "", "1", 400},
		{"missing code", `{"error":{"message":"bad"}}`, "Bad Request", "", 400},
		{"code wrong type", `{"error":{"message":"bad","code":"x"}}`, "Bad Request", "", 400},
		{"flat message", `{"message":"bad"}`, "Bad Request", "", 400},
		{"null body", `null`, "Bad Gateway", "", 502},
		{"array body", `[]`, "Bad Gateway", "", 502},
		{"not json", `oops`, "Bad Gateway", "", 502},
		{"empty", ``, "Service Unavailable", "", 503},
	} {
		msg, code := NestedErrorMessage(c.status, []byte(c.body))
		if msg != c.wantMsg || code != c.wantCode {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, msg, code, c.wantMsg, c.wantCode)
		}
	}
}

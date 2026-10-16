package fal

import "testing"

func TestParseFalErrorMessageFollowsTSSchema(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"schema", `{"error":{"message":"bad","code":1}}`, "bad"},
		{"empty message kept", `{"error":{"message":"","code":1}}`, ""},
		{"missing code", `{"error":{"message":"bad"}}`, "Bad Request"},
		{"detail string", `{"detail":"x"}`, "Bad Request"},
		{"plain text", `oops`, "Bad Request"},
	} {
		if got := parseFalErrorMessage([]byte(c.body), "Bad Request"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

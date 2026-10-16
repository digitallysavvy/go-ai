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

func TestFalImageErrorMessageFollowsTSUnion(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"validation", `{"detail":[{"loc":["body","prompt"],"msg":"required","type":"missing"},{"loc":["a"],"msg":"bad","type":"t"}]}`, "body.prompt: required\na: bad"},
		{"message", `{"message":"quota"}`, "quota"},
		{"validation missing type falls to message", `{"detail":[{"loc":["a"],"msg":"bad"}],"message":"m"}`, "m"},
		{"validation missing type without message", `{"detail":[{"loc":["a"],"msg":"bad"}]}`, "Bad Request"},
		{"non string loc", `{"detail":[{"loc":[1],"msg":"bad","type":"t"}]}`, "Bad Request"},
		{"error string is off schema", `{"error":"x"}`, "Bad Request"},
		{"plain text", `oops`, "Bad Request"},
	} {
		if got := falImageErrorMessage(400, []byte(c.body)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

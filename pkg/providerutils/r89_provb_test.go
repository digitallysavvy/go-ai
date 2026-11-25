package providerutils

import (
	"testing"
)

// TS openaiErrorDataSchema / openaiCompatibleErrorDataSchema membership.
func TestR89provbOpenAICompatibleErrorMessage(t *testing.T) {
	for body, want := range map[string]struct {
		msg string
		ok  bool
	}{
		`{"error":{"message":"m","type":"t","code":"c","param":1}}`: {"m", true},
		`{"error":{"message":"","code":42}}`:                        {"", true},
		`{"error":{"message":"m","type":5}}`:                        {"", false},
		`{"error":{"message":"m","code":true}}`:                     {"", false},
		`{"message":"m"}`:                                           {"", false},
		`{"error":"m"}`:                                             {"", false},
		`{"error":{"message":"m"},"__proto__":{}}`:                  {"", false},
		`<html/>`: {"", false},
	} {
		if msg, ok := OpenAICompatibleErrorMessage([]byte(body)); msg != want.msg || ok != want.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", body, msg, ok, want.msg, want.ok)
		}
	}
}

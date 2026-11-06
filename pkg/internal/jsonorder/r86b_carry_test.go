package jsonorder

import "testing"

// JSON.parse throws on any trailing text after the value, also text that is
// not a JSON token (cr-r86b-provb-to-carry).
func TestR86bcarryParseRejectsTrailingGarbage(t *testing.T) {
	for _, raw := range []string{`{"a":1} x`, `{"a":1}}`, `[1]]`, `1 2`, `{"a":1} {`} {
		if _, ok := Parse([]byte(raw)); ok {
			t.Errorf("Parse(%q) ok, want false (JSON.parse throws)", raw)
		}
	}
	for _, raw := range []string{`{"a":1}`, " [1] \n", `"x"`} {
		if _, ok := Parse([]byte(raw)); !ok {
			t.Errorf("Parse(%q) not ok", raw)
		}
	}
}

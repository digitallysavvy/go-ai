package providerutils

import (
	"encoding/json"
	"testing"
)

// JS String(value) for decoded JSON values (node: String(JSON.parse(x))).
func TestR87bintegJSString(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`null`, "null"}, {`"s"`, "s"}, {`true`, "true"}, {`false`, "false"},
		{`5`, "5"}, {`2.5`, "2.5"}, {`1e21`, "1e+21"}, {`-0`, "0"}, {`1e400`, "Infinity"},
		{`{"a":1}`, "[object Object]"}, {`[1,[2,null],{"a":1},true]`, "1,2,,[object Object],true"}, {`[]`, ""},
	} {
		var v interface{}
		if err := json.Unmarshal([]byte(tc.in), &v); err != nil && tc.in != `1e400` {
			t.Fatal(err)
		}
		if tc.in == `1e400` {
			v = json.Number("1e400")
		}
		if got := JSString(v); got != tc.want {
			t.Errorf("JSString(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := JSString(json.Number("12")); got != "12" {
		t.Errorf("json.Number = %q", got)
	}
}

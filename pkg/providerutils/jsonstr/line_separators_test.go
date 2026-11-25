package jsonstr

import "testing"

// JSON.stringify writes U+2028 and U+2029 as is; Go's encoder escapes them.
func TestMarshalWritesLineSeparatorsRaw(t *testing.T) {
	ls, ps := string(rune(0x2028)), string(rune(0x2029))
	bs := string(rune(92))
	cases := []struct{ name, in, want string }{
		{"raw separators", "a" + ls + "b" + ps + "c", `"a` + ls + "b" + ps + `c"`},
		{"backslash then text u2028", "a" + bs + "u2028b", `"a` + bs + bs + `u2028b"`},
		{"two backslashes then text", "a" + bs + bs + "u2028", `"a` + bs + bs + bs + bs + `u2028"`},
		{"backslash then separator", bs + ls, `"` + bs + bs + ls + `"`},
		{"other escapes", "q\" \té", `"q` + bs + `" ` + bs + `t` + "é" + `"`},
	}
	for _, c := range cases {
		got, err := Marshal(c.in)
		if err != nil || string(got) != c.want {
			t.Errorf("%s: Marshal = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	got, _ := Marshal(map[string]interface{}{"k" + ls: []string{ps}})
	if want := `{"k` + ls + `":["` + ps + `"]}`; string(got) != want {
		t.Errorf("nested = %q, want %q", got, want)
	}
}

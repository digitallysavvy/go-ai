package googlevertex

import "testing"

// TS parseOffsetSeconds is Number.parseFloat(offset): see the Google twin.
func TestPortprovgLiveOffsetReadsAsJSString(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
		ok   bool
	}{
		{"0.280s", 0.28, true},
		{float64(2), 2, true},
		{[]interface{}{"2s"}, 2, true},
		{[]interface{}{float64(1.5), float64(9)}, 1.5, true},
		{[]interface{}{nil, "3s"}, 0, false},
		{true, 0, false},
		{map[string]interface{}{"a": 1}, 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := liveOffsetSeconds(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%#v: got %v %v, want %v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

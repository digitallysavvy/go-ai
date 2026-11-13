package jsonstr

import (
	"math"
	"testing"
)

// JSON.stringify writes NaN and +-Infinity as null at every position;
// json.Marshal fails the whole value.
func TestR96carryMarshalNonFiniteAsNull(t *testing.T) {
	type inner struct {
		F   float64  `json:"f"`
		F32 float32  `json:"f32"`
		P   *float64 `json:"p,omitempty"`
		S   string   `json:"s"`
	}
	inf := math.Inf(1)
	cases := []struct {
		name string
		v    interface{}
		want string
	}{
		{"map", map[string]interface{}{"a": math.Inf(1), "b": 1.5, "c": "<x>"}, `{"a":null,"b":1.5,"c":"<x>"}`},
		{"nested", map[string]interface{}{"p": map[string]interface{}{"score": math.Inf(-1)}, "l": []interface{}{math.NaN(), 2}}, `{"l":[null,2],"p":{"score":null}}`},
		{"struct", inner{F: math.NaN(), F32: float32(math.Inf(1)), P: &inf, S: "2.5353012e+30"}, `{"f":null,"f32":null,"p":null,"s":"2.5353012e+30"}`},
		{"scalar", math.Inf(1), `null`},
		{"slice", []float64{1, math.Inf(-1)}, `[1,null]`},
	}
	for _, tc := range cases {
		got, err := Marshal(tc.v)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestR96carryMarshalNonFiniteKeepsOtherErrors(t *testing.T) {
	if _, err := Marshal(map[string]interface{}{"c": make(chan int), "f": math.NaN()}); err == nil {
		t.Fatal("an unsupported type marshaled")
	}
}

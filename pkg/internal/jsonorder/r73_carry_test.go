package jsonorder

import (
	"encoding/json"
	"math"
	"testing"
)

type r73carryScore float64

// TS toJSONValue writes NaN and Infinity as null wherever they sit; typed Go
// containers (not only map[string]interface{} and []interface{}) must keep
// the rest of the value instead of failing as a whole.
func TestR73Carry_ToJSONValueNonFiniteInTypedContainers(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		in   interface{}
		want string
	}{
		{map[string]float64{"a": nan, "b": 2}, `{"a":null,"b":2}`},
		{map[string][]float64{"s": {1, math.Inf(-1)}}, `{"s":[1,null]}`},
		{[]float32{float32(math.Inf(1)), 0.5}, `[null,0.5]`},
		{[2]float64{nan, 3}, `[null,3]`},
		{&map[string]float64{"p": nan}, `{"p":null}`},
		{[]r73carryScore{r73carryScore(nan), 1}, `[null,1]`},
		{map[string]interface{}{"n": map[string]float64{"x": nan}}, `{"n":{"x":null}}`},
	}
	for _, c := range cases {
		got, err := json.Marshal(ToJSONValue(c.in))
		if err != nil || string(got) != c.want {
			t.Errorf("ToJSONValue(%v) = %s (%v), want %s", c.in, got, err, c.want)
		}
	}
}

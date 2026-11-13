package jsonorder

import (
	"encoding/json"
	"math"
	"testing"
)

// TS toJSONValue is JSON.parse(JSON.stringify(value)): NaN and Infinity
// become null in place; the rest of the value is kept (encoding/json rejected
// the whole value, so Go returned nil).
func TestR72ProvToJSONValueNonFiniteNumbersAreNull(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{map[string]interface{}{"a": math.NaN(), "b": 1.5, "c": []interface{}{math.Inf(1), "x", map[string]interface{}{"d": math.Inf(-1)}}}, `{"a":null,"b":1.5,"c":[null,"x",{"d":null}]}`},
		{math.NaN(), `null`},
		{[]float64{1, math.NaN()}, `[1,null]`},
		{float32(math.Inf(1)), `null`},
		{map[string]interface{}{"ok": true}, `{"ok":true}`},
	}
	for _, c := range cases {
		got, _ := json.Marshal(ToJSONValue(c.in))
		if string(got) != c.want {
			t.Errorf("ToJSONValue(%v) = %s, want %s", c.in, got, c.want)
		}
	}
	if ToJSONValue(make(chan int)) != nil {
		t.Error("an unencodable value should give nil (TS JSON.stringify undefined -> null)")
	}
}

package jsonstr

import (
	"math"
	"testing"
)

// JSON.stringify writes -0 as 0 (node: JSON.stringify({a:-0,b:[-0,0.0,-0.0],
// c:JSON.parse('-0')}) === '{"a":0,"b":[0,0,0],"c":0}'); encoding/json keeps
// the sign. Strings and other numbers stay as they are.
func TestR97carryMarshalNegativeZeroAsZero(t *testing.T) {
	negZero := math.Copysign(0, -1)
	type withField struct {
		F float64 `json:"f"`
		S string  `json:"s"`
	}
	v := map[string]interface{}{
		"a": negZero,
		"b": []interface{}{negZero, 0.0, -0.5, -10.0},
		"c": withField{F: negZero, S: "-0 2024-01-02 \"-0\""},
		"d": math.Inf(-1),
		"e": float32(negZero),
	}
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":0,"b":[0,0,-0.5,-10],"c":{"f":0,"s":"-0 2024-01-02 \"-0\""},"d":null,"e":0}`
	if string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

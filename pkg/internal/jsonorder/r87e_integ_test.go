package jsonorder

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

type r87eintegInner struct {
	X float64
	Y string
}

type r87eintegOuter struct {
	A     float64
	B     string
	List  []r87eintegInner
	Ptr   *r87eintegInner
	Omit  float64 `json:"omit,omitempty"`
	F32   float32
	Inf   float64
	Keep  float64
	Named map[string]r87eintegInner
}

// cr-r87e-gen-to-carry 3: TS JSON.parse(JSON.stringify(v)) writes NaN and
// ±Inf as null and keeps the object's other fields; a struct field holding
// NaN no longer turns the whole value into null.
func TestR87eintegToJSONValueStructNaN(t *testing.T) {
	if got, want := ToJSONValue(struct {
		A float64
		B string
	}{math.NaN(), "x"}), map[string]interface{}{"A": nil, "B": "x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flat: %#v, want %#v", got, want)
	}
	// The first sentinel already occurs in the value: the next one is used.
	first := math.Ldexp(1, 101)
	v := r87eintegOuter{
		A: math.NaN(), B: "b",
		List:  []r87eintegInner{{X: math.Inf(1), Y: "y"}, {X: 1.5}},
		Ptr:   &r87eintegInner{X: math.NaN()},
		Omit:  math.NaN(),
		F32:   float32(math.NaN()),
		Inf:   math.Inf(-1),
		Keep:  first,
		Named: map[string]r87eintegInner{"k": {X: math.NaN(), Y: "z"}},
	}
	got := ToJSONValue(v)
	want := map[string]interface{}{
		"A": nil, "B": "b",
		"List":  []interface{}{map[string]interface{}{"X": nil, "Y": "y"}, map[string]interface{}{"X": 1.5, "Y": ""}},
		"Ptr":   map[string]interface{}{"X": nil, "Y": ""},
		"omit":  nil,
		"F32":   nil,
		"Inf":   nil,
		"Keep":  first,
		"Named": map[string]interface{}{"k": map[string]interface{}{"X": nil, "Y": "z"}},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Fatalf("nested:\n got %s\nwant %s", g, w)
	}
	// The input is not modified.
	if !math.IsNaN(v.A) || !math.IsNaN(v.Ptr.X) || !math.IsInf(v.List[0].X, 1) {
		t.Fatal("ToJSONValue modified its input")
	}
}

// r87eintegCustom marshals an unexported field: the copy keeps it.
type r87eintegCustom struct {
	V      float64
	secret string
}

func (c r87eintegCustom) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{"s": c.secret, "v": c.V})
}

// Custom marshalers stay encoding/json's and see the value's unexported
// fields.
func TestR87eintegToJSONValueStructNaNCustomMarshaler(t *testing.T) {
	got := ToJSONValue(r87eintegCustom{V: math.NaN(), secret: "kept"})
	if want := map[string]interface{}{"s": "kept", "v": nil}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

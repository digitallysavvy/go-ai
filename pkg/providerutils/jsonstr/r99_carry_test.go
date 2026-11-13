package jsonstr

import (
	"math"
	"testing"
)

type r99carryInner struct {
	F float64
	g float64
}

type r99carryDeep struct{ r99carryInner }

// JSON.stringify writes NaN / +-Inf as null (ruling 4). encoding/json encodes
// the exported fields promoted from an unexported embedded struct (or
// pointer to one), but the non-finite copy skipped the unsettable field, so
// Marshal failed with "json: unsupported value" (cr-r99-gen-to-carry).
func TestR99carryNonFiniteUnderUnexportedEmbeddedStruct(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{
		{struct{ r99carryInner }{r99carryInner{F: math.Inf(1), g: 1}}, `{"F":null}`},
		{struct {
			Type string
			*r99carryInner
		}{"t", &r99carryInner{F: math.NaN()}}, `{"Type":"t","F":null}`},
		{struct{ r99carryDeep }{r99carryDeep{r99carryInner{F: math.Inf(-1)}}}, `{"F":null}`},
		{struct{ *r99carryInner }{nil}, `{}`},
	} {
		got, err := Marshal(tc.v)
		if err != nil || string(got) != tc.want {
			t.Errorf("Marshal(%#v) = %s, %v; want %s", tc.v, got, err, tc.want)
		}
	}
	// The caller's value is not modified.
	in := &r99carryInner{F: math.Inf(1)}
	if _, err := Marshal(struct{ *r99carryInner }{in}); err != nil || !math.IsInf(in.F, 1) {
		t.Errorf("err = %v, F = %v", err, in.F)
	}
}

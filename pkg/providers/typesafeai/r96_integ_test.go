package typesafeai

import "testing"

// carry CR (round 96): the score legend uses the shared zodlite.JSON
// (z.json()): 1e400 there fails, a finite value passes.
func TestR96integScoreLegendSharedJSONValue(t *testing.T) {
	body := func(legend string) []byte {
		return []byte(`{"answers":{"q":{"type":"score","score":3,"probabilities":{"3":1},"legend":` + legend + `}}}`)
	}
	if err := validateDecisionResponse(200, nil, body(`{"3":["a",1,true,null]}`)); err != nil {
		t.Fatalf("finite legend: err = %v", err)
	}
	if err := validateDecisionResponse(200, nil, body(`{"3":{"x":-1e400}}`)); err == nil {
		t.Fatal("1e400 legend: err = nil, want Invalid JSON response")
	}
}

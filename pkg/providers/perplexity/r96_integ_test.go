package perplexity

import "testing"

// provg CR §2 (round 96): the score legend is
// z.record(z.string(), z.json()).nullish(): a non-finite number (JSON.parse
// of 1e400) fails the response schema, a finite one passes.
func TestR96integDecisionLegendJSONValue(t *testing.T) {
	body := func(legend string) []byte {
		return []byte(`{"answers":{"q":{"type":"score","score":3,"probabilities":{"3":1},"legend":` + legend + `}}}`)
	}
	if err := validatePerplexityDecisionResponse(200, nil, body(`{"3":{"label":"ok","w":[1]}}`)); err != nil {
		t.Fatalf("finite legend: err = %v", err)
	}
	if err := validatePerplexityDecisionResponse(200, nil, body(`{"3":{"w":[1e400]}}`)); err == nil {
		t.Fatal("1e400 legend: err = nil, want Invalid JSON response")
	}
}

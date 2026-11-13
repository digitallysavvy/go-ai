package typesafeai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR96provcScoreLegendSchema: typesafeDecisionResponseSchema declares
// score.legend as z.record(z.string(), z.json()).nullish(), so a legend that
// is not a record fails the response handler ("Invalid JSON response"), while
// a record of JSON values and null pass.
func TestR96provcScoreLegendSchema(t *testing.T) {
	ok := []string{
		`{"answers":{"q":{"type":"score","score":1,"probabilities":{},"legend":{"0":"low","1":{"m":[1,null,true]}}}}}`,
		`{"answers":{"q":{"type":"score","score":1,"probabilities":{},"legend":null}}}`,
	}
	for _, body := range ok {
		if err := validateDecisionResponse(200, nil, []byte(body)); err != nil {
			t.Errorf("%s: err = %v", body, err)
		}
	}
	err := validateDecisionResponse(200, nil, []byte(`{"answers":{"q":{"type":"score","score":1,"probabilities":{},"legend":"x"}}}`))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" {
		t.Fatalf("err = %v, want Invalid JSON response", err)
	}
}

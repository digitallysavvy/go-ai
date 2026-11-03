package perplexity

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS countImages reads the JSON object form of the state, so an image_url
// object whose image_url is a typed Go map (same JSON) is a valid image, and
// one with a non-data URL is still rejected.
func TestR83provaDecisionStateTypedImageURLObject(t *testing.T) {
	ok := map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,AQID"}}
	got, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionJSONPart(ok)})
	if err != nil {
		t.Fatalf("typed image_url map: %v", err)
	}
	if m, _ := got.(map[string]interface{}); m["type"] != "image_url" {
		t.Fatalf("state = %#v, want the object as native state", got)
	}
	type imageURL struct {
		URL string `json:"url"`
	}
	bad := map[string]interface{}{"type": "image_url", "image_url": imageURL{URL: "https://example.com/a.png"}}
	var unsupported *providererrors.UnsupportedFunctionalityError
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionJSONPart(bad)}); !errors.As(err, &unsupported) {
		t.Fatalf("non-data image URL in a struct: err = %v, want UnsupportedFunctionalityError", err)
	}
	nine := make([]interface{}, 9)
	for i := range nine {
		nine[i] = ok
	}
	var invalid *providererrors.InvalidArgumentError
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionJSONPart(nine)}); !errors.As(err, &invalid) {
		t.Fatalf("9 typed images: err = %v, want InvalidArgumentError", err)
	}
}

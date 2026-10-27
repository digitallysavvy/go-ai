package typesafeai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS builds `assets` in insertion order, with array-index keys first (a JS
// object); it was written in sorted key order.
func TestR44_AssetsKeepInsertionOrder(t *testing.T) {
	_, assets, _, err := typeSafeRequestState([]provider.DecisionStatePart{
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[b]"}),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[a]"}),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "10"}),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "2"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(assets)
	want := `{"2":{"type":"image","part":"image-3"},"10":{"type":"image","part":"image-2"},"[b]":{"type":"image","part":"image-0"},"[a]":{"type":"image","part":"image-1"}}`
	if string(raw) != want {
		t.Fatalf("assets = %s\nwant     %s", raw, want)
	}
}

// TS runs each file part's media-type check before any part resumes from
// `await parseProviderOptions`, so a later unsupported image is reported ahead
// of an option error on an earlier one.
func TestR44_MediaTypeErrorOfLaterPartWinsOverEarlierOptionError(t *testing.T) {
	_, _, _, err := typeSafeRequestState([]provider.DecisionStatePart{
		imageFile("image/png", "iVBORw==", map[string]interface{}{"insertReference": false}),
		imageFile("image/gif", "R0lGOD", nil),
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

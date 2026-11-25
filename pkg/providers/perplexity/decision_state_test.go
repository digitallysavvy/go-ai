package perplexity

import (
	"reflect"
	"testing"

	"errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func imageState(mediaType, data string) provider.DecisionStatePart {
	return provider.DecisionFilePart(types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: data}})
}

var pngImage = map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw=="}}

// Ported from convert-to-perplexity-decision-state.test.ts.
func TestConvertDecisionState_Native(t *testing.T) {
	cases := []struct {
		name  string
		state []provider.DecisionStatePart
		want  interface{}
	}{
		{"text", []provider.DecisionStatePart{provider.DecisionTextPart("Review")}, "Review"},
		{"object", []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"t": "R"})}, map[string]interface{}{"t": "R"}},
		{"array", []provider.DecisionStatePart{provider.DecisionJSONPart([]interface{}{"R", nil, 1.0})}, []interface{}{"R", nil, 1.0}},
		{"null", []provider.DecisionStatePart{provider.DecisionJSONPart(nil)}, []interface{}{nil}},
		{"false", []provider.DecisionStatePart{provider.DecisionJSONPart(false)}, []interface{}{false}},
		{"number", []provider.DecisionStatePart{provider.DecisionJSONPart(1.0)}, []interface{}{1.0}},
		{"empty", []provider.DecisionStatePart{}, []interface{}{}},
		{"image only", []provider.DecisionStatePart{imageState("image/png", "iVBORw==")}, pngImage},
		{"detected", []provider.DecisionStatePart{imageState("image/*", "iVBORw==")}, pngImage},
		{"ordered", []provider.DecisionStatePart{provider.DecisionTextPart("Inspect"), provider.DecisionJSONPart(map[string]interface{}{"p": "Vase"}), imageState("image/png", "iVBORw=="), provider.DecisionTextPart("Look")},
			[]interface{}{"Inspect", map[string]interface{}{"p": "Vase"}, pngImage, "Look"}},
	}
	for _, c := range cases {
		got, err := convertToPerplexityDecisionState(c.state)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v err %v, want %#v", c.name, got, err, c.want)
		}
	}
}

func TestConvertDecisionState_Limits(t *testing.T) {
	photos := make([]interface{}, 7)
	for i := range photos {
		photos[i] = pngImage
	}
	state := []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"photos": photos}), imageState("image/png", "iVBORw==")}
	if got, err := convertToPerplexityDecisionState(state); err != nil || len(got.([]interface{})) != 2 {
		t.Fatalf("8 images: %v %v", got, err)
	}
	var invalid *providererrors.InvalidArgumentError
	if _, err := convertToPerplexityDecisionState(append(state, provider.DecisionJSONPart(pngImage))); !errors.As(err, &invalid) {
		t.Fatalf("9 images: %v", err)
	}
	var unsupported *providererrors.UnsupportedFunctionalityError
	for _, url := range []string{"https://example.com/image.png", "data:image/gif;base64,AQID", "data:image/png,unencoded"} {
		part := provider.DecisionJSONPart(map[string]interface{}{"photo": map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}})
		if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{part}); !errors.As(err, &unsupported) {
			t.Errorf("%s: %v", url, err)
		}
	}
	for _, mt := range []string{"image/gif", "application/pdf", "audio/wav", "image/*"} {
		if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{imageState(mt, "AQID")}); !errors.As(err, &unsupported) {
			t.Errorf("%s: %v", mt, err)
		}
	}
	urlPart := provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/i.png"}})
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{urlPart}); !errors.As(err, &unsupported) {
		t.Errorf("url: %v", err)
	}
}

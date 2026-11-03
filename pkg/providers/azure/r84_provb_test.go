package azure

import (
	"context"
	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// The TS case suites of this package evaluate the extended assertion forms.
func init() { tsfixture.EnableExtendedAsserts() }

// TS parseSize is size.split('x').map(Number) and dimensionsForAspectRatio
// reads aspectRatio.split(':').map(Number): a part that is not a JS number is
// NaN (JSON null, "NaN" in the edit form), a size without "x" has no height,
// extra parts are ignored and JS whitespace around a number is allowed.
// Expected values from Node running the TS functions.
func TestR84provbMaiSizeAndAspectRatioUseJSNumber(t *testing.T) {
	server, c := newMaiServer(t, nil)
	model := maiImageModel(t, server, "MAI-Image-2.6")
	for _, tc := range []struct {
		size, aspect string
		want         map[string]interface{}
	}{
		{size: "1024", want: map[string]interface{}{"width": float64(1024)}},
		{size: "abcx5", want: map[string]interface{}{"width": nil, "height": float64(5)}},
		{size: " 512 x0x10", want: map[string]interface{}{"width": float64(512), "height": float64(0)}},
		{size: "1024.5x768x9", want: map[string]interface{}{"width": 1024.5, "height": float64(768)}},
		{aspect: "16:9:1", want: map[string]interface{}{"width": float64(1360), "height": float64(768)}},
		{aspect: " 16 : 9", want: map[string]interface{}{"width": float64(1360), "height": float64(768)}},
		{aspect: "Infinity:1", want: map[string]interface{}{"width": nil, "height": nil}},
	} {
		result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Size: tc.size, AspectRatio: tc.aspect})
		if err != nil {
			t.Fatal(err)
		}
		tc.want["model"], tc.want["prompt"] = "MAI-Image-2.6", "p"
		if !reflect.DeepEqual(c.jsonBody, tc.want) {
			t.Errorf("%q/%q: body = %#v, want %#v", tc.size, tc.aspect, c.jsonBody, tc.want)
		}
		if len(result.Warnings) != 0 {
			t.Errorf("%q/%q: warnings = %#v", tc.size, tc.aspect, result.Warnings)
		}
	}

	// The edit form writes String(value): NaN for a non-number part.
	if _, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p", Size: "abcx5",
		Files: []provider.ImageFile{{Type: "file", Data: []byte{0x89, 'P', 'N', 'G'}, MediaType: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := c.form["width"]; len(got) != 1 || got[0] != "NaN" {
		t.Errorf("form width = %#v, want NaN", c.form["width"])
	}
	if got := c.form["height"]; len(got) != 1 || got[0] != "5" {
		t.Errorf("form height = %#v, want 5", c.form["height"])
	}
}

package bfl

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provcVideoWarningsQuoteLikeTS: the TS warnings are template literals
// with `"${value}"` (black-forest-labs-video-model.ts 305-341), so a value
// with a quote or a non-ASCII rune is copied as is, not Go-escaped (%q).
func TestR90provcVideoWarningsQuoteLikeTS(t *testing.T) {
	m := newVideoTestModel(t)
	_, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Prompt:      "a cat",
		Resolution:  `wide"é`,
		AspectRatio: `5"7`,
	})
	if err != nil {
		t.Fatalf("buildVideoArgs: %v", err)
	}
	want := map[string]string{
		"resolution":  `Unrecognized resolution "wide"é". FLUX 3 video supports "hd" and "fhd", or a {width}x{height} value to map onto one.`,
		"aspectRatio": `FLUX 3 video does not support the aspect ratio "5"7". Using the provider default (auto).`,
	}
	got := map[string]string{}
	for _, w := range warnings {
		got[w.Feature] = w.Details
	}
	for feature, details := range want {
		if got[feature] != details {
			t.Errorf("%s warning = %q, want %q", feature, got[feature], details)
		}
	}
}

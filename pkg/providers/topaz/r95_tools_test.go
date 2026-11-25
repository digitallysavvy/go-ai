package topaz

import (
	"context"
	"testing"
)

// TS topaz-video-model.ts addUnsupportedWarnings warns for a prompt that is
// not nullish and not blank after trim(). A direct DoStart caller that sets Prompt
// without PromptSet gets the prompt warning too.
func TestR95toolsTopazVideoPromptWarningWithoutPromptSet(t *testing.T) {
	var bodies []map[string]interface{}
	srv := r92toolsTopazExpressServer(t, &bodies)
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), VideoModelStarlightPrecise26)
	opts := r92toolsTopazURLStart("1920x1080")
	opts.Prompt = "make it sharper"
	res, err := model.DoStart(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, w := range res.Warnings {
		if w.Feature == "prompt" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("prompt warnings = %d, want 1 (%+v)", got, res.Warnings)
	}
	// An empty prompt, set or not, does not warn.
	for _, set := range []bool{false, true} {
		o := r92toolsTopazURLStart("1920x1080")
		o.PromptSet = set
		res, err := model.DoStart(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range res.Warnings {
			if w.Feature == "prompt" {
				t.Fatalf("PromptSet=%v: unexpected prompt warning", set)
			}
		}
	}
}

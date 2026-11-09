package topaz

import (
	"context"
	"testing"
)

// TS topaz-image-model.ts parseSize (`size.split('x').map(Number)`) and
// appendIfDefined (String(value)): a fractional size keeps its fraction, a
// missing height part is 0, and a size without "x" sends only the width.
// Go parsed "%dx%d" and dropped both fields for any other shape.
func TestR90toolsTopazSizeSplitLikeJS(t *testing.T) {
	for _, tc := range []struct{ size, width, height string }{
		{"1024.5x768", "1024.5", "768"},
		{"1024x", "1024", "0"},
		{"2048", "2048", ""},
	} {
		ts := newImageTestServer(t)
		opts := defaultImageOptions(ts)
		opts.Size = tc.size
		if _, err := ts.model().DoGenerate(context.Background(), opts); err != nil {
			t.Fatalf("%s: %v", tc.size, err)
		}
		form := ts.getMultipartForm()
		if got := form["output_width"]; len(got) != 1 || got[0] != tc.width {
			t.Errorf("%s: output_width = %v, want %q", tc.size, got, tc.width)
		}
		got := form["output_height"]
		if (tc.height == "" && len(got) != 0) || (tc.height != "" && (len(got) != 1 || got[0] != tc.height)) {
			t.Errorf("%s: output_height = %v, want %q", tc.size, got, tc.height)
		}
		ts.server.Close()
	}
}

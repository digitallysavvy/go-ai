package topaz

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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

// TS topaz-video-model.ts 153-178 postJsonToApi stringifies {source:
// {container, duration, frameCount, frameRate, resolution, external},
// output: buildOutput(...), filters: [{model, ...}]} in that key order;
// Go marshalled maps with sorted keys.
func TestR90toolsTopazVideoExpressBodyKeyOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"requestId":"req-1"}`))
	}))
	defer srv.Close()
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), VideoModelStarlightPrecise26)
	opts := defaultStartOptions()
	opts.InputReferences = []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/in.mp4", MediaType: "video/mp4"}}
	if _, err := model.DoStart(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	want := `{"source":{"container":"mp4","duration":10,"frameCount":300,"frameRate":30,"resolution":{"width":1920,"height":1080},"external":{"provider":"s3","presignedUrl":"https://example.com/in.mp4"}},"output":{"resolution":{"width":1920,"height":1080},"frameRate":30,"audioTransfer":"Copy","audioCodec":"AAC","container":"mp4"},"filters":[{"model":`
	if !strings.HasPrefix(body, want) {
		t.Errorf("body\n got %s\nwant prefix %s", body, want)
	}
}

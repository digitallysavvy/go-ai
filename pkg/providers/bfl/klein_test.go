package bfl

import (
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports the "FLUX 3 Klein" block of black-forest-labs-image-model.test.ts
// (TS #23410): unknown model IDs default to the latest image API.

func (fs *flux3Server) generateKlein(t *testing.T, opts *provider.ImageGenerateOptions) error {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: fs.URL, PollIntervalMillis: 1})
	opts.Prompt = "a cat"
	res, err := NewImageModel(p, "flux-3-image-klein").DoGenerate(t.Context(), opts)
	if err == nil && len(res.Warnings) > 0 {
		fs.warnings = res.Warnings
	}
	return err
}

func TestKlein_ModelAPI(t *testing.T) {
	legacy := []string{"flux-pro", "flux-dev", "flux-pro-1.1", "flux-pro-1.0-fill", "flux-kontext-pro", "flux-2-pro", "flux-2-flex", "flux-1.1-pro", "flux-2"}
	for _, id := range legacy {
		if bflModelAPIFor(id) != bflAPILegacy {
			t.Errorf("%s should be legacy", id)
		}
	}
	if bflModelAPIFor("flux-3-image") != bflAPIFlux3 {
		t.Error("flux-3-image keeps its own API")
	}
	for _, id := range []string{"flux-3-image-klein", "test-model", "flux-4", "flux-20"} {
		if bflModelAPIFor(id) != bflAPILatest {
			t.Errorf("%s should default to latest", id)
		}
	}
}

func TestKlein_DefaultsLeftToEndpoint(t *testing.T) {
	fs := newFlux3Server(t)
	if err := fs.generateKlein(t, &provider.ImageGenerateOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := fs.lastBody(); !reflect.DeepEqual(got, map[string]interface{}{"prompt": "a cat"}) || len(fs.warnings) != 0 {
		t.Fatalf("body=%#v warnings=%v", got, fs.warnings)
	}
}

func TestKlein_ResolutionTiers(t *testing.T) {
	for _, res := range []string{"768sq", "1k", "1.5k", "2k", "4k"} {
		fs := newFlux3Server(t)
		err := fs.generateKlein(t, &provider.ImageGenerateOptions{AspectRatio: "16:9", ProviderOptions: bflOpts(map[string]interface{}{"resolution": res})})
		want := map[string]interface{}{"prompt": "a cat", "aspect_ratio": "16:9", "resolution": res}
		if err != nil || !reflect.DeepEqual(fs.lastBody(), want) || len(fs.warnings) != 0 {
			t.Fatalf("%s: err=%v body=%#v", res, err, fs.lastBody())
		}
	}
}

func TestKlein_ReferenceImages(t *testing.T) {
	fs := newFlux3Server(t)
	err := fs.generateKlein(t, &provider.ImageGenerateOptions{Files: []provider.ImageFile{
		{Type: "url", URL: "https://example.com/reference.png"},
		{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []interface{}{"https://example.com/reference.png", "AQID"}
	if got := fs.lastBody()["images"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("images = %#v", got)
	}
	if _, ok := fs.lastBody()["input_image"]; ok {
		t.Fatal("legacy input_image must not be sent")
	}
}

func TestKlein_SeedFormatStepsSafety(t *testing.T) {
	fs := newFlux3Server(t)
	seed := 42
	err := fs.generateKlein(t, &provider.ImageGenerateOptions{Seed: &seed, ProviderOptions: bflOpts(map[string]interface{}{
		"outputFormat": "png", "steps": 16, "safetyTolerance": 7,
	})})
	want := map[string]interface{}{"prompt": "a cat", "seed": 42.0, "output_format": "png", "steps": 16.0, "safety_tolerance": 7.0}
	if err != nil || !reflect.DeepEqual(fs.lastBody(), want) || len(fs.warnings) != 0 {
		t.Fatalf("err=%v body=%#v warnings=%v", err, fs.lastBody(), fs.warnings)
	}
}

func TestKlein_SizeAndDimensions(t *testing.T) {
	for _, size := range []string{"768x768", "1024x1024", "4096x4096"} {
		fs := newFlux3Server(t)
		if err := fs.generateKlein(t, &provider.ImageGenerateOptions{Size: size}); err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(size, "x")
		b := fs.lastBody()
		if b["width"].(float64) != atof(parts[0]) || b["height"].(float64) != atof(parts[1]) || len(b) != 3 || len(fs.warnings) != 0 {
			t.Fatalf("%s: %#v %v", size, b, fs.warnings)
		}
	}

	// Exact dimensions win over aspect ratio and resolution.
	fs := newFlux3Server(t)
	err := fs.generateKlein(t, &provider.ImageGenerateOptions{Size: "1536x640", AspectRatio: "3:1", ProviderOptions: bflOpts(map[string]interface{}{"resolution": "4k"})})
	if err != nil || !reflect.DeepEqual(fs.lastBody(), map[string]interface{}{"prompt": "a cat", "width": 1536.0, "height": 640.0}) {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}

	// Provider dimensions beat size.
	fs = newFlux3Server(t)
	err = fs.generateKlein(t, &provider.ImageGenerateOptions{Size: "1024x1024", ProviderOptions: bflOpts(map[string]interface{}{"width": 6256, "height": 2688})})
	if err != nil || fs.lastBody()["width"] != 6256.0 || fs.lastBody()["height"] != 2688.0 {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}

	// A lone provider width takes the other dimension from size.
	fs = newFlux3Server(t)
	err = fs.generateKlein(t, &provider.ImageGenerateOptions{Size: "1024x64", ProviderOptions: bflOpts(map[string]interface{}{"width": 64})})
	if err != nil || fs.lastBody()["width"] != 64.0 || fs.lastBody()["height"] != 64.0 {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}
}

func atof(s string) float64 {
	var n float64
	for _, c := range s {
		n = n*10 + float64(c-'0')
	}
	return n
}

func TestKlein_AspectRatios(t *testing.T) {
	fs := newFlux3Server(t)
	if err := fs.generateKlein(t, &provider.ImageGenerateOptions{AspectRatio: "7:3"}); err != nil || fs.lastBody()["aspect_ratio"] != "21:9" {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}
	fs = newFlux3Server(t)
	if err := fs.generateKlein(t, &provider.ImageGenerateOptions{AspectRatio: "3:1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.lastBody()["aspect_ratio"]; ok || len(fs.warnings) != 1 || fs.warnings[0].Feature != "aspectRatio" {
		t.Fatalf("body=%#v warnings=%v", fs.lastBody(), fs.warnings)
	}
}

func TestKlein_UnsupportedOptionsWarn(t *testing.T) {
	fs := newFlux3Server(t)
	err := fs.generateKlein(t, &provider.ImageGenerateOptions{ProviderOptions: bflOpts(map[string]interface{}{
		"grounding": false, "version": "latest", "guidance": 4, "inputImage": "dGVzdA==",
	})})
	if err != nil || !reflect.DeepEqual(fs.lastBody(), map[string]interface{}{"prompt": "a cat"}) {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}
	got := map[string]bool{}
	for _, w := range fs.warnings {
		got[w.Feature] = true
	}
	for _, f := range []string{"grounding", "version", "guidance", "inputImage"} {
		if !got["blackForestLabs."+f] {
			t.Errorf("missing warning for %s", f)
		}
	}
	if len(fs.warnings) != 4 {
		t.Fatalf("warnings = %v", fs.warnings)
	}
}

func TestKlein_RejectsBeforeSubmission(t *testing.T) {
	invalid := []map[string]interface{}{
		{"width": 63, "height": 64}, {"width": 64, "height": 6257}, {"width": 6256, "height": 6256},
		{"width": 1024}, {"height": 1024}, {"steps": 0}, {"steps": 17},
		{"safetyTolerance": 8}, {"resolution": "8k"},
	}
	for _, o := range invalid {
		fs := newFlux3Server(t)
		err := fs.generateKlein(t, &provider.ImageGenerateOptions{ProviderOptions: bflOpts(o)})
		if err == nil || len(fs.submits) != 0 {
			t.Fatalf("%v: err=%v submits=%d", o, err, len(fs.submits))
		}
	}
	for _, size := range []string{"63x64", "6257x64", "4096x4108"} {
		fs := newFlux3Server(t)
		err := fs.generateKlein(t, &provider.ImageGenerateOptions{Size: size})
		if err == nil || len(fs.submits) != 0 {
			t.Fatalf("%s: err=%v submits=%d", size, err, len(fs.submits))
		}
	}
	fs := newFlux3Server(t)
	mask := provider.ImageFile{Type: "url", URL: "https://example.com/mask.png"}
	err := fs.generateKlein(t, &provider.ImageGenerateOptions{Mask: &mask})
	if err == nil || !strings.Contains(err.Error(), "FLUX 3 image masks") || len(fs.submits) != 0 {
		t.Fatalf("mask: err=%v", err)
	}
	files := make([]provider.ImageFile, 11)
	for i := range files {
		files[i] = provider.ImageFile{Type: "url", URL: "https://example.com/image.png"}
	}
	err = fs.generateKlein(t, &provider.ImageGenerateOptions{Files: files})
	if err == nil || err.Error() != "Black Forest Labs supports up to 10 input images." || len(fs.submits) != 0 {
		t.Fatalf("files: err=%v", err)
	}
}

// TS builds width/height with Number(size part) and spreads the explicit
// provider options over them, so a size Number() cannot read only fails when
// no explicit option replaces it.
func TestKlein_UnreadableSizeIsReplacedByExplicitDimensions(t *testing.T) {
	fs := newFlux3Server(t)
	err := fs.generateKlein(t, &provider.ImageGenerateOptions{Size: "abc", ProviderOptions: bflOpts(map[string]interface{}{"width": 512, "height": 256})})
	if err != nil || fs.lastBody()["width"] != 512.0 || fs.lastBody()["height"] != 256.0 {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}
	// Only one dimension replaced: the other one still comes from size.
	fs = newFlux3Server(t)
	err = fs.generateKlein(t, &provider.ImageGenerateOptions{Size: "abcx128", ProviderOptions: bflOpts(map[string]interface{}{"width": 512})})
	if err != nil || fs.lastBody()["width"] != 512.0 || fs.lastBody()["height"] != 128.0 {
		t.Fatalf("err=%v body=%#v", err, fs.lastBody())
	}
	for _, size := range []string{"abc", "1024", "1024x", "1.5x64"} {
		fs = newFlux3Server(t)
		err = fs.generateKlein(t, &provider.ImageGenerateOptions{Size: size})
		if err == nil || len(fs.submits) != 0 {
			t.Fatalf("%q: err=%v submits=%d", size, err, len(fs.submits))
		}
	}
}

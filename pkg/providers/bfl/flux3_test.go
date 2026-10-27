package bfl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type flux3Server struct {
	*httptest.Server
	mu         sync.Mutex
	submits    []map[string]interface{}
	downloadHd http.Header
	pollCount  int
	pollFn     func(n int, w http.ResponseWriter, r *http.Request)
	warnings   []types.Warning
}

func newFlux3Server(t *testing.T) *flux3Server {
	t.Helper()
	fs := &flux3Server{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		switch r.URL.Path {
		case "/flux-3-image", "/flux-pro", "/flux-3-image-klein":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fs.submits = append(fs.submits, body)
			_, _ = w.Write([]byte(`{"id":"req-123","polling_url":"http://` + r.Host + `/poll"}`))
		case "/poll":
			fs.pollCount++
			if fs.pollFn != nil {
				fs.pollFn(fs.pollCount, w, r)
				return
			}
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"http://` + r.Host + `/image.png"}}`))
		case "/image.png":
			fs.downloadHd = r.Header.Clone()
			_, _ = w.Write([]byte("image"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *flux3Server) generate(t *testing.T, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: fs.URL, PollIntervalMillis: 1})
	if opts.Prompt == "" {
		opts.Prompt = "a cat"
	}
	return NewImageModel(p, "flux-3-image").DoGenerate(context.Background(), opts)
}

func (fs *flux3Server) lastBody() map[string]interface{} {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.submits[len(fs.submits)-1]
}

func bflOpts(m map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"blackForestLabs": m}
}

func TestFlux3_DefaultsLeftToEndpoint(t *testing.T) {
	fs := newFlux3Server(t)
	res, err := fs.generate(t, &provider.ImageGenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fs.lastBody(), map[string]interface{}{"prompt": "a cat"}) {
		t.Fatalf("body = %#v", fs.lastBody())
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
}

func TestFlux3_ProviderOptions(t *testing.T) {
	for _, resolution := range []string{"768sq", "1k", "1.5k", "2k", "4k"} {
		fs := newFlux3Server(t)
		_, err := fs.generate(t, &provider.ImageGenerateOptions{
			AspectRatio:     "16:9",
			ProviderOptions: bflOpts(map[string]interface{}{"resolution": resolution, "grounding": false, "safetyTolerance": 4, "version": "latest"}),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]interface{}{"prompt": "a cat", "aspect_ratio": "16:9", "resolution": resolution, "grounding": false, "safety_tolerance": 4.0, "version": "latest"}
		if !reflect.DeepEqual(fs.lastBody(), want) {
			t.Fatalf("body = %#v", fs.lastBody())
		}
	}
}

func TestFlux3_ReferenceImages(t *testing.T) {
	fs := newFlux3Server(t)
	_, err := fs.generate(t, &provider.ImageGenerateOptions{Files: []provider.ImageFile{
		{Type: "url", URL: "https://example.com/reference.png"},
		{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"prompt": "a cat", "images": []interface{}{"https://example.com/reference.png", "AQID"}}
	if !reflect.DeepEqual(fs.lastBody(), want) {
		t.Fatalf("body = %#v", fs.lastBody())
	}
}

func TestFlux3_SizeAndAspectRatio(t *testing.T) {
	seed := 42
	cases := []struct {
		name      string
		opts      provider.ImageGenerateOptions
		wantRatio interface{}
		wantWarn  []string // features, in order
	}{
		{"derive from size, seed warned", provider.ImageGenerateOptions{Size: "1536x1024", Seed: &seed}, "3:2", []string{"size", "seed"}},
		{"explicit ratio beats size", provider.ImageGenerateOptions{Size: "1024x1024", AspectRatio: "16:9"}, "16:9", []string{"size"}},
		{"unsupported 7:4", provider.ImageGenerateOptions{Size: "1792x1024"}, nil, []string{"size", "aspectRatio"}},
		{"unsupported 12:5", provider.ImageGenerateOptions{Size: "1536x640"}, nil, []string{"size", "aspectRatio"}},
		{"unsupported explicit with supported size", provider.ImageGenerateOptions{Size: "1536x1024", AspectRatio: "3:1"}, nil, []string{"size", "aspectRatio"}},
		{"21:9 from size", provider.ImageGenerateOptions{Size: "2520x1080"}, "21:9", []string{"size"}},
		{"9:21 from size", provider.ImageGenerateOptions{Size: "1080x2520"}, "9:21", []string{"size"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFlux3Server(t)
			opts := tc.opts
			res, err := fs.generate(t, &opts)
			if err != nil {
				t.Fatal(err)
			}
			body := fs.lastBody()
			if body["aspect_ratio"] != tc.wantRatio || body["width"] != nil || body["height"] != nil || body["seed"] != nil {
				t.Fatalf("body = %#v", body)
			}
			var got []string
			for _, w := range res.Warnings {
				got = append(got, w.Feature)
			}
			if !reflect.DeepEqual(got, tc.wantWarn) {
				t.Fatalf("warnings = %#v", res.Warnings)
			}
		})
	}

	fs := newFlux3Server(t)
	res, err := fs.generate(t, &provider.ImageGenerateOptions{Size: "1792x1024"})
	if err != nil {
		t.Fatal(err)
	}
	want := "FLUX 3 does not support aspect ratio 7:4. Using the endpoint's default auto aspect ratio."
	if res.Warnings[1].Details != want {
		t.Fatalf("details = %q", res.Warnings[1].Details)
	}
}

func TestFlux3_SupportedAspectRatios(t *testing.T) {
	for _, ar := range []string{"21:9", "2:1", "16:9", "3:2", "7:5", "4:3", "5:4", "1:1", "4:5", "3:4", "5:7", "2:3", "9:16", "1:2", "9:21"} {
		fs := newFlux3Server(t)
		res, err := fs.generate(t, &provider.ImageGenerateOptions{AspectRatio: ar})
		if err != nil {
			t.Fatal(err)
		}
		if fs.lastBody()["aspect_ratio"] != ar || len(res.Warnings) != 0 {
			t.Fatalf("%s: body=%#v warnings=%#v", ar, fs.lastBody(), res.Warnings)
		}
	}
}

func TestFlux3_LegacyOptionsWarn(t *testing.T) {
	fs := newFlux3Server(t)
	res, err := fs.generate(t, &provider.ImageGenerateOptions{ProviderOptions: bflOpts(map[string]interface{}{
		"width": 1024, "height": 1024, "outputFormat": "png", "steps": 20, "guidance": 4,
		"promptUpsampling": true, "raw": false, "imagePrompt": "dGVzdA==", "imagePromptStrength": 0.5,
		"inputImage": "dGVzdA==", "webhookUrl": "https://example.com/webhook", "webhookSecret": "secret",
	})})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fs.lastBody(), map[string]interface{}{"prompt": "a cat"}) {
		t.Fatalf("body = %#v", fs.lastBody())
	}
	if len(res.Warnings) != 12 {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
	found := map[string]bool{}
	for _, w := range res.Warnings {
		found[w.Feature] = true
	}
	for _, f := range []string{"blackForestLabs.width", "blackForestLabs.raw", "blackForestLabs.inputImage"} {
		if !found[f] {
			t.Fatalf("missing warning %s", f)
		}
	}
}

func TestFlux3_RejectsBeforeSubmission(t *testing.T) {
	invalid := []map[string]interface{}{
		{"safetyTolerance": 8}, {"safetyTolerance": -1}, {"safetyTolerance": 1.5},
		{"resolution": "8k"}, {"version": "unknown"},
	}
	for _, o := range invalid {
		fs := newFlux3Server(t)
		_, err := fs.generate(t, &provider.ImageGenerateOptions{ProviderOptions: bflOpts(o)})
		if err == nil || len(fs.submits) != 0 {
			t.Fatalf("%v: err=%v submits=%d", o, err, len(fs.submits))
		}
	}

	fs := newFlux3Server(t)
	mask := provider.ImageFile{Type: "url", URL: "https://example.com/mask.png"}
	_, err := fs.generate(t, &provider.ImageGenerateOptions{Mask: &mask})
	if err == nil || !strings.Contains(err.Error(), "FLUX 3 image masks") || !providererrors.IsUnsupportedFunctionalityError(err) || err.Error() != "'FLUX 3 image masks' functionality not supported." || len(fs.submits) != 0 {
		t.Fatalf("mask: err=%v submits=%d", err, len(fs.submits))
	}

	files := make([]provider.ImageFile, 11)
	for i := range files {
		files[i] = provider.ImageFile{Type: "url", URL: "https://example.com/image.png"}
	}
	_, err = fs.generate(t, &provider.ImageGenerateOptions{Files: files})
	if err == nil || len(fs.submits) != 0 {
		t.Fatalf("files: err=%v", err)
	}
}

func TestFlux3_LegacySafetyToleranceRangePreserved(t *testing.T) {
	fs := newFlux3Server(t)
	p := New(Config{APIKey: "k", BaseURL: fs.URL, PollIntervalMillis: 1})
	_, err := NewImageModel(p, "flux-pro").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x", ProviderOptions: bflOpts(map[string]interface{}{"safetyTolerance": 6}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fs.lastBody()["safety_tolerance"] != 6.0 {
		t.Fatalf("body = %#v", fs.lastBody())
	}
}

func TestFlux3_PollsThroughReasoningAndGenerating(t *testing.T) {
	fs := newFlux3Server(t)
	statuses := []string{"Pending", "Reasoning", "Generating", "Ready"}
	fs.pollFn = func(n int, w http.ResponseWriter, r *http.Request) {
		st := statuses[n-1]
		if st == "Ready" {
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"http://` + r.Host + `/image.png","duration":2}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"` + st + `"}`))
	}
	res, err := fs.generate(t, &provider.ImageGenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fs.pollCount != 4 || len(res.Images) != 1 {
		t.Fatalf("pollCount=%d images=%d", fs.pollCount, len(res.Images))
	}
}

func TestFlux3_StopsOnModerationStatuses(t *testing.T) {
	for _, status := range []string{"Request Moderated", "Content Moderated", "Task not found"} {
		fs := newFlux3Server(t)
		fs.pollFn = func(n int, w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
		}
		_, err := fs.generate(t, &provider.ImageGenerateOptions{})
		want := "Black Forest Labs generation failed: " + status + "."
		if err == nil || err.Error() != want || fs.pollCount != 1 {
			t.Fatalf("%s: err=%v polls=%d", status, err, fs.pollCount)
		}
	}
}

func TestFlux3_Http503TerminalStatusIsNotRetryable(t *testing.T) {
	for _, field := range []string{"status", "state"} {
		fs := newFlux3Server(t)
		fs.pollFn = func(n int, w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"` + field + `":"Error"}`))
		}
		_, err := fs.generate(t, &provider.ImageGenerateOptions{})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.StatusCode != 503 || perr.IsRetryable() || perr.Message != "Black Forest Labs generation failed: Error." || fs.pollCount != 1 {
			t.Fatalf("%s: err=%v polls=%d", field, err, fs.pollCount)
		}
	}
}

func TestFlux3_CustomProxyDownloadKeepsHeaders(t *testing.T) {
	for _, model := range []string{"flux-pro", "flux-3-image"} {
		fs := newFlux3Server(t)
		p := New(Config{APIKey: "k", BaseURL: fs.URL, PollIntervalMillis: 1, Headers: map[string]string{"Custom-Provider-Header": "provider"}})
		_, err := NewImageModel(p, model).DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "x", Headers: map[string]string{"Custom-Request-Header": "request"},
		})
		if err != nil {
			t.Fatal(err)
		}
		h := fs.downloadHd
		if h.Get("X-Key") != "k" || h.Get("Custom-Provider-Header") != "provider" || h.Get("Custom-Request-Header") != "request" {
			t.Fatalf("%s: download headers = %#v", model, h)
		}
	}
}

func TestBFLHostIsBFL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://api.bfl.ai/v1": true, "https://bfl.ai": true, "https://api.example.com": false, "http://localhost:1": false,
	} {
		if bflHostIsBFL(u) != want {
			t.Errorf("bflHostIsBFL(%q) != %v", u, want)
		}
	}
}

package bytedance

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r95provcImageBody returns the raw /images/generations body the image model
// sends for opts.
func r95provcImageBody(t *testing.T, opts *provider.ImageGenerateOptions) string {
	t.Helper()
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	im, _ := p.ImageModel("seedream-4-0-250828")
	if _, err := im.DoGenerate(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	return body
}

// TS doGenerate builds the body as an object literal plus assignments, and
// JSON.stringify keeps that insertion order (a key assigned again keeps its
// first position): model, prompt, image, size, the provider options, the
// passthrough options, response_format.
func TestR95provcImageBodyKeyOrder(t *testing.T) {
	options := map[string]interface{}{"bytedance": map[string]interface{}{
		"watermark":                 false,
		"outputFormat":              "png",
		"size":                      "2K",
		"sequentialImageGeneration": "auto",
		"maxImages":                 3,
		"optimizePromptMode":        "standard",
		"customKey":                 1,
	}}
	got := r95provcImageBody(t, &provider.ImageGenerateOptions{
		Prompt:          "p",
		Size:            "1024x1024",
		Files:           []provider.ImageFile{{Type: "url", URL: "https://x/i.png"}},
		ProviderOptions: options,
	})
	want := `{"model":"seedream-4-0-250828","prompt":"p","image":"https://x/i.png","size":"2K","watermark":false,"output_format":"png","sequential_image_generation":"auto","sequential_image_generation_options":{"max_images":3},"optimize_prompt_options":{"mode":"standard"},"customKey":1,"response_format":"b64_json"}`
	if got != want {
		t.Errorf("body\n got %s\nwant %s", got, want)
	}

	// Without a top-level size, the resolution level is first set after
	// output_format.
	got = r95provcImageBody(t, &provider.ImageGenerateOptions{
		Prompt:          "p",
		ProviderOptions: map[string]interface{}{"bytedance": map[string]interface{}{"outputFormat": "jpeg", "size": "2K"}},
	})
	want = `{"model":"seedream-4-0-250828","prompt":"p","output_format":"jpeg","size":"2K","response_format":"b64_json"}`
	if got != want {
		t.Errorf("body\n got %s\nwant %s", got, want)
	}
}

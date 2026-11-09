package deepinfra

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS deepinfra-image-model.ts 149-156 postJsonToApi body {prompt,
// num_images, aspect_ratio, width, height, seed, ...options} in that key
// order; Go marshalled a map with sorted keys.
func TestR90toolsDeepInfraImageBodyKeyOrder(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"images":["AQID"]}`))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
	n, seed := 1, 7
	if _, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat", N: &n, Size: "1024x768", AspectRatio: "4:3", Seed: &seed,
	}); err != nil {
		t.Fatal(err)
	}
	if want := `{"prompt":"a cat","num_images":1,"aspect_ratio":"4:3","width":"1024","height":"768","seed":7}`; body != want {
		t.Errorf("body\n got %s\nwant %s", body, want)
	}
}

// provb CR (cr-r90-provb-to-tools 1): TS deepinfra-image-model.ts 226
// fileToBlob downloads a URL image or mask with downloadBlob, which types
// the Blob with the response content-type. Go sent the part as
// application/octet-stream (a URL ImageFile has no media type).
func TestR90toolsDeepInfraEditURLPartType(t *testing.T) {
	types := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if part.FileName() != "" {
				types[part.FormName()] = part.Header.Get("Content-Type")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"AQID"}]}`))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
	n := 1
	mask := provider.ImageFile{Type: "url", URL: "data:image/png;base64,AQI="}
	if _, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat", N: &n,
		Files: []provider.ImageFile{{Type: "url", URL: "data:image/webp;base64,AQI="}},
		Mask:  &mask,
	}); err != nil {
		t.Fatal(err)
	}
	if types["image"] != "image/webp" || types["mask"] != "image/png" {
		t.Fatalf("part types = %v", types)
	}
}

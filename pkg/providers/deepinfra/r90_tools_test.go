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

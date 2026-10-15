package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// azure.image() wraps OpenAIImageModel in TS: decoded b64 images (all of
// them), usage, providerOptions.openai, api-version query, max images.
func TestImageModelDelegatesToOpenAIImage(t *testing.T) {
	var path, query string
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		b := base64.StdEncoding.EncodeToString([]byte("img1"))
		c := base64.StdEncoding.EncodeToString([]byte("img2"))
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"` + b + `"},{"b64_json":"` + c + `"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`))
	}))
	defer srv.Close()

	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai", APIVersion: "2025-01-01-preview", UseDeploymentBasedURLs: true})
	m := NewImageModel(p, "gpt-image-1")
	if m.MaxImagesPerCall() != 10 {
		t.Fatalf("MaxImagesPerCall = %d, want 10", m.MaxImagesPerCall())
	}
	if NewImageModel(p, "my-deployment").MaxImagesPerCall() != 1 {
		t.Fatal("unknown deployment should be 1")
	}
	n := 2
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "cat", N: &n,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"background": "transparent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/openai/deployments/gpt-image-1/images/generations" || query != "api-version=2025-01-01-preview" {
		t.Fatalf("path=%q query=%q", path, query)
	}
	if body["background"] != "transparent" {
		t.Fatalf("provider options not forwarded: %v", body)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("gpt-image-* must not send response_format: %v", body)
	}
	if len(res.Images) != 2 || string(res.Images[1]) != "img2" || res.Usage.TotalTokens != 7 {
		t.Fatalf("result = %+v", res)
	}
}

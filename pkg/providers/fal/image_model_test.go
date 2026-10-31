package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestImageModelBuildRequestBody(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "fal-fast")
	n := 3
	seed := 9
	body, warnings, err := m.buildRequest(&provider.ImageGenerateOptions{
		Prompt: "city skyline",
		Size:   "800x600",
		N:      &n,
		Seed:   &seed,
		Files: []provider.ImageFile{
			{Type: "file", Data: []byte("a"), MediaType: "image/png"},
			{Type: "url", URL: "https://x/y.png"},
		},
		Mask: &provider.ImageFile{Type: "file", Data: []byte("m"), MediaType: "image/png"},
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{
			"guidance_scale": 5.0, "outputFormat": "png", "custom": "x", "useMultipleImages": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["prompt"] != "city skyline" || body["num_images"] != 3 || body["seed"] != 9 {
		t.Fatalf("unexpected body: %+v", body)
	}
	size, ok := body["image_size"].(map[string]interface{})
	if !ok || size["width"] != 800 || size["height"] != 600 {
		t.Fatalf("image_size not parsed: %+v", body["image_size"])
	}
	if body["image_url"] != "data:image/png;base64,YQ==" || body["mask_url"] != "data:image/png;base64,bQ==" {
		t.Fatalf("files: %+v", body)
	}
	if body["guidance_scale"] != 5.0 || body["output_format"] != "png" || body["custom"] != "x" {
		t.Fatalf("options not mapped: %+v", body)
	}
	if _, ok := body["useMultipleImages"]; ok {
		t.Fatal("useMultipleImages must not be sent")
	}
	if len(warnings) != 2 || !strings.Contains(warnings[1].Message, "'guidance_scale' (use 'guidanceScale')") {
		t.Fatalf("warnings = %+v", warnings)
	}

	ar, _, _ := m.buildRequest(&provider.ImageGenerateOptions{Prompt: "p", AspectRatio: "16:9"})
	if ar["image_size"] != "landscape_16_9" {
		t.Fatalf("aspect ratio: %v", ar["image_size"])
	}
	multi, _, _ := m.buildRequest(&provider.ImageGenerateOptions{
		Prompt:          "p",
		Files:           []provider.ImageFile{{Type: "url", URL: "u1"}, {Type: "url", URL: "u2"}},
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{"useMultipleImages": true}},
	})
	if urls, _ := multi["image_urls"].([]string); len(urls) != 2 {
		t.Fatalf("image_urls: %v", multi)
	}
}

func TestImageModelConvertBodyMetadataAndAllImages(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "fal-fast")
	res, err := m.convertBody(context.Background(), []byte(`{
		"images":[{"url":"data:image/png;base64,YQ==","content_type":"image/png","width":1,"height":2},{"url":"data:image/png;base64,Yg==","file_name":"f"}],
		"has_nsfw_concepts":[false,true],"seed":42,"prompt":"p","timings":{"inference":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 2 || string(res.Images[1]) != "b" || res.Usage.ImageCount != 2 {
		t.Fatalf("result = %+v", res)
	}
	fal := res.ProviderMetadata["fal"].(map[string]interface{})
	imgs := fal["images"].([]map[string]interface{})
	if imgs[0]["contentType"] != "image/png" || imgs[1]["nsfw"] != true || imgs[1]["fileName"] != "f" || imgs[0]["width"] != float64(1) {
		t.Fatalf("images meta = %+v", imgs)
	}
	if fal["seed"] != float64(42) || fal["prompt"] != nil {
		t.Fatalf("response meta = %+v", fal)
	}
	if _, err := m.convertBody(context.Background(), []byte(`{"image":{"url":"data:image/png;base64,YQ=="}}`)); err != nil {
		t.Fatalf("single image response: %v", err)
	}
	if _, err := m.convertBody(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected no images error")
	}
}

func TestImageModelDoGenerate(t *testing.T) {
	var status int
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fal-fast" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write([]byte(`{"images":[{"url":"data:image/png;base64,ZmFrZWltYWdl","content_type":"image/png"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"detail":[{"loc":["body","prompt"],"msg":"required","type":"x"}]}`))
	}))
	defer apiSrv.Close()

	p := New(Config{APIKey: "k", BaseURL: apiSrv.URL})
	m := NewImageModel(p, "fal-fast")

	status = 200
	okRes, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("DoGenerate success error = %v", err)
	}
	if okRes.URL == "" || len(okRes.Image) == 0 {
		t.Fatalf("unexpected success result: %+v", okRes)
	}

	status = 500
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "test"})
	if err == nil || !strings.Contains(err.Error(), "body.prompt: required") {
		t.Fatalf("expected validation error message, got %v", err)
	}
}

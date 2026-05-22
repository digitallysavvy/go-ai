package together

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestImageModelBuildRequestBodyAndConvertResponse(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "img-model")
	if m.SpecificationVersion() != "v4" || m.Provider() != "together" || m.ModelID() != "img-model" {
		t.Fatalf("unexpected model metadata")
	}

	n := 2
	seed := 1234
	body := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "a mountain",
		N:      &n,
		Seed:   &seed,
		Size:   "1024x768",
	})
	if body["model"] != "img-model" || body["prompt"] != "a mountain" || body["n"] != 2 {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body["response_format"] != "base64" {
		t.Fatalf("expected base64 response format, got %+v", body)
	}
	if body["seed"] != 1234 {
		t.Fatalf("expected seed in request body, got %+v", body)
	}
	if body["width"] != 1024 || body["height"] != 768 {
		t.Fatalf("expected parsed dimensions, got %+v", body)
	}

	body = m.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a mountain"})
	if _, ok := body["seed"]; ok {
		t.Fatalf("expected omitted seed when unset, got %+v", body)
	}
	if _, ok := body["n"]; ok {
		t.Fatalf("expected omitted n when unset, got %+v", body)
	}

	one := 1
	body = m.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a mountain", N: &one})
	if _, ok := body["n"]; ok {
		t.Fatalf("expected omitted n when set to 1, got %+v", body)
	}

	body = m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "edit",
		Files:  []provider.ImageFile{{Type: "file", MediaType: "image/png", Data: []byte("hello")}},
		ProviderOptions: map[string]interface{}{
			"togetherai": map[string]interface{}{"steps": 12, "negative_prompt": "blur"},
		},
	})
	if body["image_url"] != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("expected data URI image_url, got %+v", body)
	}
	if body["steps"] != 12 || body["negative_prompt"] != "blur" {
		t.Fatalf("expected provider options merged, got %+v", body)
	}

	_, err := m.convertResponse([]byte(`{"data":[]}`))
	if err == nil {
		t.Fatal("expected error when no images returned")
	}

	res, err := m.convertResponse([]byte(`{"data":[{"url":"https://example.com/a.png"}]}`))
	if err != nil || res.URL == "" || res.MimeType != "image/png" {
		t.Fatalf("url response conversion failed: res=%+v err=%v", res, err)
	}

	res, err = m.convertResponse([]byte(`{"data":[{"b64_json":"Zmlyc3Q="},{"b64_json":"c2Vjb25k"}]}`))
	if err != nil {
		t.Fatalf("multi-image response conversion failed: %v", err)
	}
	if string(res.Image) != "Zmlyc3Q=" || res.Base64Image != "Zmlyc3Q=" {
		t.Fatalf("first image fields = image %q base64 %q", string(res.Image), res.Base64Image)
	}
	if len(res.Images) != 2 || string(res.Images[1]) != "c2Vjb25k" {
		t.Fatalf("images = %+v, want both returned images", res.Images)
	}
	if len(res.Base64Images) != 2 || res.Base64Images[1] != "c2Vjb25k" {
		t.Fatalf("base64 images = %+v, want both returned images", res.Base64Images)
	}
	if res.Usage.ImageCount != 2 {
		t.Fatalf("image count = %d, want 2", res.Usage.ImageCount)
	}
}

func TestImageModelDoGenerate(t *testing.T) {
	var status int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"Zm9v"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "img-model")

	status = 200
	okRes, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x",
		Size:   "1024x768",
		Files: []provider.ImageFile{
			{Type: "file", MediaType: "image/png", Data: []byte("first")},
			{Type: "file", MediaType: "image/png", Data: []byte("second")},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate success path error = %v", err)
	}
	if string(okRes.Image) != "Zm9v" || okRes.MimeType != "image/png" {
		t.Fatalf("unexpected success response: %+v", okRes)
	}
	if okRes.Base64Image != "Zm9v" || len(okRes.Images) != 1 || len(okRes.Base64Images) != 1 {
		t.Fatalf("unexpected image result shape: %+v", okRes)
	}
	if len(okRes.Warnings) != 2 {
		t.Fatalf("warnings = %+v, want size and multiple image warnings", okRes.Warnings)
	}

	status = 500
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err == nil {
		t.Fatal("expected non-200 error")
	}

	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x",
		Mask:   &provider.ImageFile{Type: "file", MediaType: "image/png", Data: []byte("mask")},
	})
	if err == nil {
		t.Fatal("expected mask unsupported error")
	}
}

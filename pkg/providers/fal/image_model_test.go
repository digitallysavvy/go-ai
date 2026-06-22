package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestImageModelBuildRequestBody(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "fal-fast")
	n := 3
	body := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "city skyline",
		Size:   "800x600",
		N:      &n,
	})
	if body["prompt"] != "city skyline" || body["num_images"] != 3 {
		t.Fatalf("unexpected body: %+v", body)
	}
	size, ok := body["image_size"].(map[string]interface{})
	if !ok || size["width"] != 800 || size["height"] != 600 {
		t.Fatalf("image_size not parsed: %+v", body["image_size"])
	}
}

func TestImageModelConvertResponseAndDownload(t *testing.T) {
	imageURL := "data:image/png;base64,iVBORw=="

	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "fal-fast")

	res, err := m.convertResponse(context.Background(), falImageResponse{
		Images: []struct {
			URL         string `json:"url"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			ContentType string `json:"content_type"`
		}{
			{URL: imageURL, ContentType: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("convertResponse() error = %v", err)
	}
	if res.URL != imageURL || len(res.Image) == 0 || res.Usage.ImageCount != 1 {
		t.Fatalf("unexpected image result: %+v", res)
	}

	_, err = m.convertResponse(context.Background(), falImageResponse{})
	if err == nil {
		t.Fatal("expected no images generated error")
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
		_, _ = w.Write([]byte(`{"error":"boom"}`))
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
	if err == nil {
		t.Fatal("expected non-200 error")
	}
}

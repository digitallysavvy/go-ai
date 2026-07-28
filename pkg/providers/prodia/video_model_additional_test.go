package prodia

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestProdiaVideoModelHelpers(t *testing.T) {
	model := NewVideoModel(&Provider{}, "prodia-video")
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "prodia.video" {
		t.Fatalf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "prodia-video" {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}
	max := model.MaxVideosPerCall()
	if max == nil || *max != 1 {
		t.Fatalf("MaxVideosPerCall() = %v", max)
	}

	data, mime, err := resolveVideoImage(context.Background(), &provider.VideoModelV3File{
		Type:      "file",
		Data:      []byte{1, 2, 3},
		MediaType: "image/png",
	})
	if err != nil {
		t.Fatalf("resolveVideoImage(file) error = %v", err)
	}
	if len(data) != 3 || mime != "image/png" {
		t.Fatalf("resolveVideoImage(file) = (%v, %q)", data, mime)
	}

	if _, _, err := resolveVideoImage(context.Background(), &provider.VideoModelV3File{Type: "unknown"}); err == nil {
		t.Fatal("resolveVideoImage(unknown type) should fail")
	}
}

func TestProdiaVideoImageURLUsesValidatedDownload(t *testing.T) {
	_, _, err := resolveVideoImage(context.Background(), &provider.VideoModelV3File{
		Type: "url",
		URL:  "http://127.0.0.1/image.png",
	})
	if err == nil {
		t.Fatal("resolveVideoImage(url) should reject unsafe hosts")
	}
	if !strings.Contains(err.Error(), "URL with IP address 127.0.0.1 is not allowed") {
		t.Fatalf("resolveVideoImage(url) error = %v", err)
	}
}

func TestProdiaVideoImageURLUsesDownloadedMediaType(t *testing.T) {
	data, mimeType, err := resolveVideoImage(context.Background(), &provider.VideoModelV3File{
		Type:      "url",
		URL:       "data:image/png;base64,AQI=",
		MediaType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("resolveVideoImage(data URL) error = %v", err)
	}
	if string(data) != string([]byte{1, 2}) || mimeType != "image/png" {
		t.Fatalf("resolveVideoImage(data URL) = %v, %q; want data bytes and downloaded image/png", data, mimeType)
	}
}

func TestProdiaVideoModelNetworkErrorPath(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	model := NewVideoModel(p, "prodia-video")
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:      "test",
		AspectRatio: "16:9",
	})
	if err == nil {
		t.Fatal("DoGenerate should fail on unreachable host")
	}
}

func TestProdiaImageModelNetworkErrorPath(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	img := NewImageModel(p, "prodia-image")
	_, err := img.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a castle",
		Size:   "bad-size",
	})
	if err == nil {
		t.Fatal("Image DoGenerate should fail on unreachable host")
	}
}

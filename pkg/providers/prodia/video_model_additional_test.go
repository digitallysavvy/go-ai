package prodia

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestProdiaVideoModelHelpers(t *testing.T) {
	model := NewVideoModel(&Provider{}, "prodia-video")
	if model.SpecificationVersion() != "v3" {
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

func TestProdiaVideoModelUnsupportedAspectRatio(t *testing.T) {
	model := NewVideoModel(&Provider{}, "prodia-video")
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:      "test",
		AspectRatio: "99:99",
	})
	if err == nil {
		t.Fatal("DoGenerate should reject unsupported aspect ratio before network calls")
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

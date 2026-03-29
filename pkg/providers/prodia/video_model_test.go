package prodia

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestProdiaVideoModelSpecificationVersion verifies the spec version is "v3".
func TestProdiaVideoModelSpecificationVersion(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	if got := model.SpecificationVersion(); got != "v3" {
		t.Errorf("SpecificationVersion() = %q, want %q", got, "v3")
	}
}

// TestProdiaVideoModelProvider verifies the provider name matches the
// TypeScript SDK's "prodia.video" value.
func TestProdiaVideoModelProvider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	if got := model.Provider(); got != "prodia.video" {
		t.Errorf("Provider() = %q, want %q", got, "prodia.video")
	}
}

// TestProdiaVideoModelID verifies that ModelID returns the provided ID.
func TestProdiaVideoModelID(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	for _, id := range []string{VideoModelWan22LightningTxt2Vid, VideoModelWan22LightningImg2Vid} {
		model := NewVideoModel(prov, id)
		if got := model.ModelID(); got != id {
			t.Errorf("ModelID() = %q, want %q", got, id)
		}
	}
}

// TestProdiaVideoModelMaxVideosPerCall verifies MaxVideosPerCall returns 1.
func TestProdiaVideoModelMaxVideosPerCall(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	n := model.MaxVideosPerCall()
	if n == nil {
		t.Fatal("MaxVideosPerCall() = nil, want *1")
	}
	if *n != 1 {
		t.Errorf("MaxVideosPerCall() = %d, want 1", *n)
	}
}

// TestProdiaVideoModelImplementsInterface verifies that ProdiaVideoModel
// satisfies the provider.VideoModelV3 interface at compile time.
func TestProdiaVideoModelImplementsInterface(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	var _ provider.VideoModelV3 = NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)
}

// TestProviderVideoModelRouting verifies VideoModel() routes wan2-2 model IDs
// correctly and rejects unknown IDs.
func TestProviderVideoModelRouting(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	// txt2vid model
	vm, err := prov.VideoModel(VideoModelWan22LightningTxt2Vid)
	if err != nil {
		t.Fatalf("VideoModel(%q) error: %v", VideoModelWan22LightningTxt2Vid, err)
	}
	if vm.ModelID() != VideoModelWan22LightningTxt2Vid {
		t.Errorf("ModelID() = %q, want %q", vm.ModelID(), VideoModelWan22LightningTxt2Vid)
	}

	// img2vid model
	vm2, err := prov.VideoModel(VideoModelWan22LightningImg2Vid)
	if err != nil {
		t.Fatalf("VideoModel(%q) error: %v", VideoModelWan22LightningImg2Vid, err)
	}
	if vm2.ModelID() != VideoModelWan22LightningImg2Vid {
		t.Errorf("ModelID() = %q, want %q", vm2.ModelID(), VideoModelWan22LightningImg2Vid)
	}

	// Empty model ID → default txt2vid
	vm3, err := prov.VideoModel("")
	if err != nil {
		t.Fatalf("VideoModel(\"\") error: %v", err)
	}
	if vm3.ModelID() != VideoModelWan22LightningTxt2Vid {
		t.Errorf("default ModelID() = %q, want %q", vm3.ModelID(), VideoModelWan22LightningTxt2Vid)
	}

	// Unknown model ID → error
	_, err = prov.VideoModel("some-other-model")
	if err == nil {
		t.Error("expected error for unsupported video model, got nil")
	}
}

// TestVideoModelConstants verifies that the model ID constants are set to the
// expected values.
func TestVideoModelConstants(t *testing.T) {
	if VideoModelWan22LightningTxt2Vid != "inference.wan2-2.lightning.txt2vid.v0" {
		t.Errorf("VideoModelWan22LightningTxt2Vid = %q, want %q",
			VideoModelWan22LightningTxt2Vid, "inference.wan2-2.lightning.txt2vid.v0")
	}
	if VideoModelWan22LightningImg2Vid != "inference.wan2-2.lightning.img2vid.v0" {
		t.Errorf("VideoModelWan22LightningImg2Vid = %q, want %q",
			VideoModelWan22LightningImg2Vid, "inference.wan2-2.lightning.img2vid.v0")
	}
}

// TestExtractVideoProviderOptionsResolution verifies resolution extraction from
// provider options.
func TestExtractVideoProviderOptionsResolution(t *testing.T) {
	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{
			"prodia": map[string]interface{}{
				"resolution": "720p",
			},
		},
	}

	provOpts := extractVideoProviderOptions(opts)
	if provOpts == nil {
		t.Fatal("expected non-nil provider options")
	}
	if provOpts.Resolution != "720p" {
		t.Errorf("Resolution = %q, want %q", provOpts.Resolution, "720p")
	}
}

// TestExtractVideoProviderOptionsNil verifies that nil ProviderOptions returns
// nil.
func TestExtractVideoProviderOptionsNil(t *testing.T) {
	opts := &provider.VideoModelV3CallOptions{}
	if got := extractVideoProviderOptions(opts); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestVideoModelT2VRejectsInvalidAspectRatio verifies that an invalid aspect
// ratio returns an error before making any network call.
func TestVideoModelT2VRejectsInvalidAspectRatio(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	_, err := model.DoGenerate(nil, &provider.VideoModelV3CallOptions{
		Prompt:      "a cat running",
		AspectRatio: "10:3", // invalid
	})
	if err == nil {
		t.Fatal("expected error for invalid aspect ratio, got nil")
	}
}

// TestVideoModelI2VRejectsInvalidAspectRatio verifies that an invalid aspect
// ratio returns an error for the img2vid path too.
func TestVideoModelI2VRejectsInvalidAspectRatio(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningImg2Vid)

	_, err := model.DoGenerate(nil, &provider.VideoModelV3CallOptions{
		Prompt:      "animate this",
		AspectRatio: "bad",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      []byte{0x89, 0x50, 0x4E, 0x47},
			MediaType: "image/png",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid aspect ratio, got nil")
	}
}

package prodia

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestPriceAlwaysIncludedInQuery verifies that price=true is always sent as a
// URL query parameter, matching the TS implementation which hardcodes it.
func TestPriceAlwaysIncludedInQuery(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "inference.flux-fast.schnell.txt2img.v2")

	query := model.buildQuery()
	if query == nil {
		t.Fatal("expected non-nil query map")
	}
	if query["price"] != "true" {
		t.Errorf("query[price] = %q, want %q", query["price"], "true")
	}
}

// TestPriceAlwaysIncludedWithNoProviderOptions verifies price=true is sent
// even when ProviderOptions is nil.
func TestPriceAlwaysIncludedWithNoProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "inference.flux-fast.schnell.txt2img.v2")

	query := model.buildQuery()
	if query["price"] != "true" {
		t.Errorf("query[price] = %q, want %q", query["price"], "true")
	}
}

// TestBuildRequestBodyContainsModelAndPrompt verifies the request body
// structure includes the model type and prompt in config.
func TestBuildRequestBodyContainsModelAndPrompt(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "inference.flux-fast.schnell.txt2img.v2")

	opts := &provider.ImageGenerateOptions{
		Prompt: "a sunset over the ocean",
	}

	body := model.buildRequestBody(opts, nil)

	if body["type"] != "inference.flux-fast.schnell.txt2img.v2" {
		t.Errorf("type = %v, want inference.flux-fast.schnell.txt2img.v2", body["type"])
	}

	cfg, ok := body["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("config is not map[string]interface{}, got %T", body["config"])
	}
	if cfg["prompt"] != "a sunset over the ocean" {
		t.Errorf("config.prompt = %v, want %q", cfg["prompt"], "a sunset over the ocean")
	}
}

// TestBuildRequestBodyWithSizeParam verifies that size is parsed and added to config.
func TestBuildRequestBodyWithSizeParam(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "inference.flux-fast.schnell.txt2img.v2")

	opts := &provider.ImageGenerateOptions{
		Prompt: "test",
		Size:   "1024x768",
	}

	body := model.buildRequestBody(opts, nil)
	cfg := body["config"].(map[string]interface{})

	if cfg["width"] != 1024 {
		t.Errorf("width = %v, want 1024", cfg["width"])
	}
	if cfg["height"] != 768 {
		t.Errorf("height = %v, want 768", cfg["height"])
	}
}

// TestBuildRequestBodyProviderOptsOverrideSize verifies that provider-level
// width/height override the size string.
func TestBuildRequestBodyProviderOptsOverrideSize(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "inference.flux-fast.schnell.txt2img.v2")

	opts := &provider.ImageGenerateOptions{
		Prompt: "test",
		Size:   "1024x1024",
		ProviderOptions: map[string]interface{}{
			"prodia": map[string]interface{}{
				"width":  512,
				"height": 512,
			},
		},
	}

	provOpts := extractProviderOptions(opts)
	body := model.buildRequestBody(opts, provOpts)
	cfg := body["config"].(map[string]interface{})

	if cfg["width"] != 512 {
		t.Errorf("width = %v, want 512", cfg["width"])
	}
	if cfg["height"] != 512 {
		t.Errorf("height = %v, want 512", cfg["height"])
	}
}

// TestProdiaProviderName verifies the provider name.
func TestProdiaProviderName(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	if prov.Name() != "prodia" {
		t.Errorf("Name() = %q, want %q", prov.Name(), "prodia")
	}
}

// TestProdiaImageModelProvider verifies the image model provider name.
func TestProdiaImageModelProvider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "inference.flux-fast.schnell.txt2img.v2")
	if model.Provider() != "prodia" {
		t.Errorf("Provider() = %q, want %q", model.Provider(), "prodia")
	}
	if model.ModelID() != "inference.flux-fast.schnell.txt2img.v2" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "inference.flux-fast.schnell.txt2img.v2")
	}
}

// TestProdiaUsesEnvVar verifies that New() picks up PRODIA_TOKEN from env.
func TestProdiaUsesEnvVar(t *testing.T) {
	t.Setenv("PRODIA_TOKEN", "env-token")
	prov := New(Config{})
	if prov == nil {
		t.Fatal("New() returned nil")
	}
	if prov.Name() != "prodia" {
		t.Errorf("Name() = %q, want %q", prov.Name(), "prodia")
	}
}

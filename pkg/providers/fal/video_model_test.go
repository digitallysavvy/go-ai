package fal

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVideoModelBuildRequestBody_URLImage(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/image-to-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat walking",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/cat.jpg",
		},
		AspectRatio: "16:9",
	}

	body := model.buildRequestBody(opts)

	if body["prompt"] != "A cat walking" {
		t.Errorf("Expected prompt 'A cat walking', got %v", body["prompt"])
	}

	if body["image_url"] != "https://example.com/cat.jpg" {
		t.Errorf("Expected image_url 'https://example.com/cat.jpg', got %v", body["image_url"])
	}

	if body["aspect_ratio"] != "16:9" {
		t.Errorf("Expected aspect_ratio '16:9', got %v", body["aspect_ratio"])
	}
}

func TestVideoModelBuildRequestBody_FileImage(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/image-to-video")

	// Test data: PNG header
	imageData := []byte{0x89, 0x50, 0x4E, 0x47}

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat walking",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      imageData,
			MediaType: "image/png",
		},
		AspectRatio: "16:9",
	}

	body := model.buildRequestBody(opts)

	if body["prompt"] != "A cat walking" {
		t.Errorf("Expected prompt 'A cat walking', got %v", body["prompt"])
	}

	// Verify image_url is a data URI
	imageURL, ok := body["image_url"].(string)
	if !ok {
		t.Fatalf("Expected image_url to be a string, got %T", body["image_url"])
	}

	expectedPrefix := "data:image/png;base64,"
	if len(imageURL) < len(expectedPrefix) || imageURL[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("Expected image_url to start with %q, got %q", expectedPrefix, imageURL)
	}

	// Verify the base64 data is correct (iVBORw== for PNG header)
	expectedURL := "data:image/png;base64,iVBORw=="
	if imageURL != expectedURL {
		t.Errorf("Expected image_url %q, got %q", expectedURL, imageURL)
	}
}

func TestVideoModelBuildRequestBody_FileImageJPEG(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/image-to-video")

	// Test data: JPEG header
	imageData := []byte{0xFF, 0xD8, 0xFF}

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat walking",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      imageData,
			MediaType: "image/jpeg",
		},
	}

	body := model.buildRequestBody(opts)

	imageURL, ok := body["image_url"].(string)
	if !ok {
		t.Fatalf("Expected image_url to be a string, got %T", body["image_url"])
	}

	expectedURL := "data:image/jpeg;base64,/9j/"
	if imageURL != expectedURL {
		t.Errorf("Expected image_url %q, got %q", expectedURL, imageURL)
	}
}

func TestVideoModelBuildRequestBody_NoImage(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/text-to-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt:      "A cat walking",
		AspectRatio: "16:9",
	}

	body := model.buildRequestBody(opts)

	if body["prompt"] != "A cat walking" {
		t.Errorf("Expected prompt 'A cat walking', got %v", body["prompt"])
	}

	if _, exists := body["image_url"]; exists {
		t.Error("Expected image_url to not exist when no image provided")
	}
}

func TestVideoModelBuildRequestBody_WithParameters(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/text-to-video")

	duration := 5.0
	fps := 30
	seed := 12345

	opts := &provider.VideoModelV3CallOptions{
		Prompt:      "A cat walking",
		AspectRatio: "16:9",
		Resolution:  "1280x720",
		Duration:    &duration,
		FPS:         &fps,
		Seed:        &seed,
	}

	body := model.buildRequestBody(opts)

	if body["prompt"] != "A cat walking" {
		t.Errorf("Expected prompt 'A cat walking', got %v", body["prompt"])
	}

	if body["aspect_ratio"] != "16:9" {
		t.Errorf("Expected aspect_ratio '16:9', got %v", body["aspect_ratio"])
	}

	if body["resolution"] != "1280x720" {
		t.Errorf("Expected resolution '1280x720', got %v", body["resolution"])
	}

	if body["duration"] != 5.0 {
		t.Errorf("Expected duration 5.0, got %v", body["duration"])
	}

	if body["fps"] != 30 {
		t.Errorf("Expected fps 30, got %v", body["fps"])
	}

	if body["seed"] != 12345 {
		t.Errorf("Expected seed 12345, got %v", body["seed"])
	}
}

func TestVideoModelBuildRequestBody_ProviderOptions(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/text-to-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat walking",
		ProviderOptions: map[string]interface{}{
			"fal": map[string]interface{}{
				"customParam":    "value",
				"pollIntervalMs": 1000, // Should be excluded
				"pollTimeoutMs":  5000, // Should be excluded
			},
		},
	}

	body := model.buildRequestBody(opts)

	if body["customParam"] != "value" {
		t.Errorf("Expected customParam 'value', got %v", body["customParam"])
	}

	if _, exists := body["pollIntervalMs"]; exists {
		t.Error("Expected pollIntervalMs to be excluded from request body")
	}

	if _, exists := body["pollTimeoutMs"]; exists {
		t.Error("Expected pollTimeoutMs to be excluded from request body")
	}
}

func TestVideoModelSpecificationVersion(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/text-to-video")

	if model.SpecificationVersion() != "v3" {
		t.Errorf("Expected specification version 'v3', got %q", model.SpecificationVersion())
	}
}

func TestVideoModelProvider(t *testing.T) {
	prov := &Provider{}
	model := NewVideoModel(prov, "fal-ai/kling-video/v2.5-turbo/pro/text-to-video")

	if model.Provider() != "fal" {
		t.Errorf("Expected provider 'fal', got %q", model.Provider())
	}
}

func TestVideoModelID(t *testing.T) {
	prov := &Provider{}
	modelID := "fal-ai/kling-video/v2.5-turbo/pro/text-to-video"
	model := NewVideoModel(prov, modelID)

	if model.ModelID() != modelID {
		t.Errorf("Expected model ID %q, got %q", modelID, model.ModelID())
	}
}

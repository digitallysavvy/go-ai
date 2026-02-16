package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVideoModelDetectMode(t *testing.T) {
	tests := []struct {
		modelID string
		want    string
	}{
		{"wan2.5-t2v", "t2v"},
		{"wan2.6-t2v", "t2v"},
		{"wan2.6-i2v", "i2v"},
		{"wan2.6-i2v-flash", "i2v"},
		{"wan2.6-r2v", "r2v"},
		{"wan2.6-r2v-flash", "r2v"},
		{"unknown-model", "t2v"}, // defaults to t2v
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			cfg := Config{APIKey: "test-key"}
			prov := New(cfg)
			model := NewVideoModel(prov, tt.modelID)
			got := model.detectMode()
			if got != tt.want {
				t.Errorf("detectMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVideoModelBuildRequestBody_T2V(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-t2v")

	opts := &provider.VideoModelV3CallOptions{
		Prompt:      "A cat walking",
		AspectRatio: "16:9",
	}

	body := model.buildRequestBody(opts)

	if body["model"] != "wan2.6-t2v" {
		t.Errorf("Expected model 'wan2.6-t2v', got %v", body["model"])
	}

	input, ok := body["input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected input to be a map")
	}

	if input["text"] != "A cat walking" {
		t.Errorf("Expected text 'A cat walking', got %v", input["text"])
	}

	params, ok := body["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected parameters to be a map")
	}

	if params["aspect_ratio"] != "16:9" {
		t.Errorf("Expected aspect_ratio '16:9', got %v", params["aspect_ratio"])
	}
}

func TestVideoModelBuildRequestBody_I2V_URLImage(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-i2v")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "The cat turns its head",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/cat.jpg",
		},
	}

	body := model.buildRequestBody(opts)

	input, ok := body["input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected input to be a map")
	}

	if input["img_url"] != "https://example.com/cat.jpg" {
		t.Errorf("Expected img_url 'https://example.com/cat.jpg', got %v", input["img_url"])
	}

	if input["text"] != "The cat turns its head" {
		t.Errorf("Expected text 'The cat turns its head', got %v", input["text"])
	}
}

func TestVideoModelBuildRequestBody_I2V_FileImage(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-i2v")

	// Test data: PNG header
	imageData := []byte{0x89, 0x50, 0x4E, 0x47}

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "The cat turns its head",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      imageData,
			MediaType: "image/png",
		},
	}

	body := model.buildRequestBody(opts)

	input, ok := body["input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected input to be a map")
	}

	// Verify img_url is raw base64 (not data URI)
	imgURL, ok := input["img_url"].(string)
	if !ok {
		t.Fatalf("Expected img_url to be a string, got %T", input["img_url"])
	}

	// Should be raw base64, not data URI
	expectedBase64 := "iVBORw=="
	if imgURL != expectedBase64 {
		t.Errorf("Expected img_url %q, got %q", expectedBase64, imgURL)
	}

	// Verify it's NOT a data URI
	if len(imgURL) > 5 && imgURL[:5] == "data:" {
		t.Error("Expected raw base64, got data URI format")
	}
}

func TestVideoModelBuildRequestBody_I2V_FileImageJPEG(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-i2v-flash")

	// Test data: JPEG header
	imageData := []byte{0xFF, 0xD8, 0xFF}

	opts := &provider.VideoModelV3CallOptions{
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      imageData,
			MediaType: "image/jpeg",
		},
	}

	body := model.buildRequestBody(opts)

	input, ok := body["input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected input to be a map")
	}

	imgURL, ok := input["img_url"].(string)
	if !ok {
		t.Fatalf("Expected img_url to be a string, got %T", input["img_url"])
	}

	expectedBase64 := "/9j/"
	if imgURL != expectedBase64 {
		t.Errorf("Expected img_url %q, got %q", expectedBase64, imgURL)
	}
}

func TestVideoModelBuildRequestBody_R2V(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-r2v")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A character walking",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/reference.jpg",
		},
	}

	body := model.buildRequestBody(opts)

	input, ok := body["input"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected input to be a map")
	}

	if input["reference_image_url"] != "https://example.com/reference.jpg" {
		t.Errorf("Expected reference_image_url 'https://example.com/reference.jpg', got %v", input["reference_image_url"])
	}

	if input["text"] != "A character walking" {
		t.Errorf("Expected text 'A character walking', got %v", input["text"])
	}

	// R2V should not support file input (only URL)
	if _, exists := input["img_url"]; exists {
		t.Error("Expected R2V to not have img_url field")
	}
}

func TestVideoModelBuildRequestBody_WithDuration(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-t2v")

	duration := 5.0
	opts := &provider.VideoModelV3CallOptions{
		Prompt:   "A cat walking",
		Duration: &duration,
	}

	body := model.buildRequestBody(opts)

	params, ok := body["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected parameters to be a map")
	}

	if params["duration"] != 5.0 {
		t.Errorf("Expected duration 5.0, got %v", params["duration"])
	}
}

func TestVideoModelBuildRequestBody_ProviderOptions(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-t2v")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat walking",
		ProviderOptions: map[string]interface{}{
			"alibaba": map[string]interface{}{
				"negative_prompt": "ugly",
				"watermark":       true,
				"pollIntervalMs":  2000, // Should be excluded
				"pollTimeoutMs":   60000, // Should be excluded
			},
		},
	}

	body := model.buildRequestBody(opts)

	params, ok := body["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected parameters to be a map")
	}

	if params["negative_prompt"] != "ugly" {
		t.Errorf("Expected negative_prompt 'ugly', got %v", params["negative_prompt"])
	}

	if params["watermark"] != true {
		t.Errorf("Expected watermark true, got %v", params["watermark"])
	}

	if _, exists := params["pollIntervalMs"]; exists {
		t.Error("Expected pollIntervalMs to be excluded from parameters")
	}

	if _, exists := params["pollTimeoutMs"]; exists {
		t.Error("Expected pollTimeoutMs to be excluded from parameters")
	}
}

func TestVideoModelSpecificationVersion(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-t2v")

	if model.SpecificationVersion() != "v3" {
		t.Errorf("Expected specification version 'v3', got %q", model.SpecificationVersion())
	}
}

func TestVideoModelProvider(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	model := NewVideoModel(prov, "wan2.6-t2v")

	if model.Provider() != "alibaba.video" {
		t.Errorf("Expected provider 'alibaba.video', got %q", model.Provider())
	}
}

func TestVideoModelID(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)
	modelID := "wan2.6-i2v"
	model := NewVideoModel(prov, modelID)

	if model.ModelID() != modelID {
		t.Errorf("Expected model ID %q, got %q", modelID, model.ModelID())
	}
}

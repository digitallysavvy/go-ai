package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVideoModel_SpecificationVersion(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "gemini-2.0-flash")

	if model.SpecificationVersion() != "v3" {
		t.Errorf("Expected specification version v3, got %s", model.SpecificationVersion())
	}
}

func TestVideoModel_Provider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "gemini-2.0-flash")

	if model.Provider() != "google" {
		t.Errorf("Expected provider 'google', got %s", model.Provider())
	}
}

func TestVideoModel_ModelID(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	modelID := "gemini-2.0-flash"
	model := NewVideoModel(prov, modelID)

	if model.ModelID() != modelID {
		t.Errorf("Expected model ID %s, got %s", modelID, model.ModelID())
	}
}

func TestVideoModel_MaxVideosPerCall(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "gemini-2.0-flash")

	maxVideos := model.MaxVideosPerCall()
	if maxVideos != nil {
		t.Errorf("Expected MaxVideosPerCall to be nil (default 1), got %v", *maxVideos)
	}
}

func TestVideoModel_BuildRequestBody(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "gemini-2.0-flash")

	tests := []struct {
		name     string
		opts     *provider.VideoModelV3CallOptions
		validate func(t *testing.T, body map[string]interface{})
	}{
		{
			name: "text-to-video with prompt",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "A cat playing piano",
			},
			validate: func(t *testing.T, body map[string]interface{}) {
				if body["prompt"] != "A cat playing piano" {
					t.Errorf("Expected prompt in body")
				}
			},
		},
		{
			name: "with aspect ratio",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:      "A dog running",
				AspectRatio: "16:9",
			},
			validate: func(t *testing.T, body map[string]interface{}) {
				config, ok := body["generationConfig"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected generationConfig in body")
				}
				if config["aspectRatio"] != "16:9" {
					t.Errorf("Expected aspectRatio 16:9 in config")
				}
			},
		},
		{
			name: "with resolution mapping",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:     "A bird flying",
				Resolution: "1920x1080",
			},
			validate: func(t *testing.T, body map[string]interface{}) {
				config, ok := body["generationConfig"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected generationConfig in body")
				}
				// Should map 1920x1080 to "1080p"
				if config["resolution"] != "1080p" {
					t.Errorf("Expected resolution to be mapped to 1080p, got %v", config["resolution"])
				}
			},
		},
		{
			name: "with duration",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:   "A sunset",
				Duration: floatPtr(5.0),
			},
			validate: func(t *testing.T, body map[string]interface{}) {
				config, ok := body["generationConfig"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected generationConfig in body")
				}
				if config["durationSeconds"] != 5.0 {
					t.Errorf("Expected durationSeconds 5.0 in config")
				}
			},
		},
		{
			name: "with seed",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "A mountain",
				Seed:   intPtr(42),
			},
			validate: func(t *testing.T, body map[string]interface{}) {
				config, ok := body["generationConfig"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected generationConfig in body")
				}
				if config["seed"] != 42 {
					t.Errorf("Expected seed 42 in config")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := model.buildRequestBody(tt.opts)
			tt.validate(t, body)
		})
	}
}

func TestVideoModel_GetPollOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "gemini-2.0-flash")

	tests := []struct {
		name           string
		providerOpts   map[string]interface{}
		expectedInterval int
		expectedTimeout  int
	}{
		{
			name:           "default options",
			providerOpts:   nil,
			expectedInterval: 2000,   // default
			expectedTimeout:  300000, // default
		},
		{
			name: "custom polling options",
			providerOpts: map[string]interface{}{
				"google": map[string]interface{}{
					"pollIntervalMs": 5000,
					"pollTimeoutMs":  600000,
				},
			},
			expectedInterval: 5000,
			expectedTimeout:  600000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := model.getPollOptions(tt.providerOpts)
			if opts.PollIntervalMs != tt.expectedInterval {
				t.Errorf("Expected interval %d, got %d", tt.expectedInterval, opts.PollIntervalMs)
			}
			if opts.PollTimeoutMs != tt.expectedTimeout {
				t.Errorf("Expected timeout %d, got %d", tt.expectedTimeout, opts.PollTimeoutMs)
			}
		})
	}
}

// TestVideoModel_DoGenerate would require mocking the HTTP client
// This is left as an integration test or requires adding mock support

func TestProvider_VideoModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	t.Run("returns video model", func(t *testing.T) {
		model, err := prov.VideoModel("gemini-2.0-flash")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if model == nil {
			t.Fatal("Expected model, got nil")
		}
	})

	t.Run("returns error for empty model ID", func(t *testing.T) {
		_, err := prov.VideoModel("")
		if err == nil {
			t.Fatal("Expected error for empty model ID")
		}
	})
}

// Helper functions

func floatPtr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}

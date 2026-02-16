package klingai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestDetectMode(t *testing.T) {
	tests := []struct {
		name      string
		modelID   string
		wantMode  VideoMode
		wantError bool
	}{
		{
			name:      "text-to-video model",
			modelID:   "kling-v2.6-t2v",
			wantMode:  VideoModeT2V,
			wantError: false,
		},
		{
			name:      "image-to-video model",
			modelID:   "kling-v2.6-i2v",
			wantMode:  VideoModeI2V,
			wantError: false,
		},
		{
			name:      "motion control model",
			modelID:   "kling-v2.6-motion-control",
			wantMode:  VideoModeMotionControl,
			wantError: false,
		},
		{
			name:      "versioned t2v model",
			modelID:   "kling-v2.1-master-t2v",
			wantMode:  VideoModeT2V,
			wantError: false,
		},
		{
			name:      "invalid model ID",
			modelID:   "kling-v2.6-invalid",
			wantMode:  "",
			wantError: true,
		},
		{
			name:      "no suffix",
			modelID:   "kling-v2.6",
			wantMode:  "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := detectMode(tt.modelID)
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				if mode != tt.wantMode {
					t.Errorf("expected mode %s, got %s", tt.wantMode, mode)
				}
			}
		})
	}
}

func TestGetAPIModelName(t *testing.T) {
	tests := []struct {
		name         string
		modelID      string
		mode         VideoMode
		wantAPIName  string
	}{
		{
			name:         "simple t2v model",
			modelID:      "kling-v1-t2v",
			mode:         VideoModeT2V,
			wantAPIName:  "kling-v1",
		},
		{
			name:         "versioned t2v model with dots",
			modelID:      "kling-v2.6-t2v",
			mode:         VideoModeT2V,
			wantAPIName:  "kling-v2-6",
		},
		{
			name:         "master variant with dots",
			modelID:      "kling-v2.1-master-t2v",
			mode:         VideoModeT2V,
			wantAPIName:  "kling-v2-1-master",
		},
		{
			name:         "i2v model",
			modelID:      "kling-v2.6-i2v",
			mode:         VideoModeI2V,
			wantAPIName:  "kling-v2-6",
		},
		{
			name:         "motion control model",
			modelID:      "kling-v2.6-motion-control",
			mode:         VideoModeMotionControl,
			wantAPIName:  "kling-v2-6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				AccessKey: "test-ak",
				SecretKey: "test-sk",
			}
			prov, _ := New(cfg)
			model := &VideoModel{
				prov:    prov,
				modelID: tt.modelID,
				mode:    tt.mode,
			}

			apiName := model.getAPIModelName()
			if apiName != tt.wantAPIName {
				t.Errorf("expected API name %s, got %s", tt.wantAPIName, apiName)
			}
		})
	}
}

func TestGetEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		mode         VideoMode
		wantEndpoint string
	}{
		{
			name:         "text-to-video endpoint",
			mode:         VideoModeT2V,
			wantEndpoint: "/v1/videos/text2video",
		},
		{
			name:         "image-to-video endpoint",
			mode:         VideoModeI2V,
			wantEndpoint: "/v1/videos/image2video",
		},
		{
			name:         "motion control endpoint",
			mode:         VideoModeMotionControl,
			wantEndpoint: "/v1/videos/motion-control",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				AccessKey: "test-ak",
				SecretKey: "test-sk",
			}
			prov, _ := New(cfg)
			model := &VideoModel{
				prov:    prov,
				modelID: "test-model",
				mode:    tt.mode,
			}

			endpoint := model.getEndpoint()
			if endpoint != tt.wantEndpoint {
				t.Errorf("expected endpoint %s, got %s", tt.wantEndpoint, endpoint)
			}
		})
	}
}

func TestVideoModelInterface(t *testing.T) {
	cfg := Config{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	prov, _ := New(cfg)
	model := &VideoModel{
		prov:    prov,
		modelID: "kling-v2.6-t2v",
		mode:    VideoModeT2V,
	}

	t.Run("SpecificationVersion returns v3", func(t *testing.T) {
		if model.SpecificationVersion() != "v3" {
			t.Errorf("expected v3, got %s", model.SpecificationVersion())
		}
	})

	t.Run("Provider returns klingai", func(t *testing.T) {
		if model.Provider() != "klingai" {
			t.Errorf("expected klingai, got %s", model.Provider())
		}
	})

	t.Run("ModelID returns correct ID", func(t *testing.T) {
		if model.ModelID() != "kling-v2.6-t2v" {
			t.Errorf("expected kling-v2.6-t2v, got %s", model.ModelID())
		}
	})

	t.Run("MaxVideosPerCall returns nil", func(t *testing.T) {
		if model.MaxVideosPerCall() != nil {
			t.Error("expected nil for MaxVideosPerCall")
		}
	})
}

func TestBuildT2VBody(t *testing.T) {
	cfg := Config{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	prov, _ := New(cfg)
	model := &VideoModel{
		prov:    prov,
		modelID: "kling-v2.6-t2v",
		mode:    VideoModeT2V,
	}

	t.Run("basic t2v request", func(t *testing.T) {
		duration := 5.0
		opts := &provider.VideoModelV3CallOptions{
			Prompt:      "A sunrise over mountains",
			AspectRatio: "16:9",
			Duration:    &duration,
		}

		body, warnings, err := model.buildT2VBody(opts, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["prompt"] != "A sunrise over mountains" {
			t.Error("prompt not set correctly")
		}

		if body["aspect_ratio"] != "16:9" {
			t.Error("aspect_ratio not set correctly")
		}

		if body["duration"] != "5" {
			t.Error("duration not set correctly")
		}

		if body["model_name"] != "kling-v2-6" {
			t.Errorf("expected model_name kling-v2-6, got %v", body["model_name"])
		}

		if len(warnings) > 0 {
			t.Errorf("expected no warnings, got %d", len(warnings))
		}
	})

	t.Run("t2v with provider options", func(t *testing.T) {
		mode := "pro"
		negPrompt := "low quality"
		cfgScale := 0.8

		opts := &provider.VideoModelV3CallOptions{
			Prompt: "test",
		}

		provOpts := &ProviderOptions{
			Mode:           &mode,
			NegativePrompt: &negPrompt,
			CfgScale:       &cfgScale,
		}

		body, _, err := model.buildT2VBody(opts, provOpts)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["mode"] != "pro" {
			t.Error("mode not set correctly")
		}

		if body["negative_prompt"] != "low quality" {
			t.Error("negative_prompt not set correctly")
		}

		if body["cfg_scale"] != 0.8 {
			t.Error("cfg_scale not set correctly")
		}
	})

	t.Run("t2v with image shows warning", func(t *testing.T) {
		opts := &provider.VideoModelV3CallOptions{
			Prompt: "test",
			Image: &provider.VideoModelV3File{
				Type: "url",
				URL:  "https://example.com/image.png",
			},
		}

		_, warnings, err := model.buildT2VBody(opts, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(warnings) == 0 {
			t.Error("expected warning for image in t2v")
		}
	})
}

func TestBuildI2VBody(t *testing.T) {
	cfg := Config{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	prov, _ := New(cfg)
	model := &VideoModel{
		prov:    prov,
		modelID: "kling-v2.6-i2v",
		mode:    VideoModeI2V,
	}

	t.Run("basic i2v request with URL", func(t *testing.T) {
		opts := &provider.VideoModelV3CallOptions{
			Prompt: "Pan the camera slowly",
			Image: &provider.VideoModelV3File{
				Type: "url",
				URL:  "https://example.com/image.png",
			},
		}

		body, warnings, err := model.buildI2VBody(opts, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["prompt"] != "Pan the camera slowly" {
			t.Error("prompt not set correctly")
		}

		if body["image"] != "https://example.com/image.png" {
			t.Error("image URL not set correctly")
		}

		if len(warnings) > 0 {
			t.Errorf("expected no warnings, got %d", len(warnings))
		}
	})

	t.Run("i2v with end frame", func(t *testing.T) {
		imageTail := "https://example.com/end.png"
		opts := &provider.VideoModelV3CallOptions{
			Prompt: "test",
			Image: &provider.VideoModelV3File{
				Type: "url",
				URL:  "https://example.com/start.png",
			},
		}

		provOpts := &ProviderOptions{
			ImageTail: &imageTail,
		}

		body, _, err := model.buildI2VBody(opts, provOpts)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["image_tail"] != imageTail {
			t.Error("image_tail not set correctly")
		}
	})

	t.Run("i2v with aspect ratio shows warning", func(t *testing.T) {
		opts := &provider.VideoModelV3CallOptions{
			AspectRatio: "16:9",
		}

		_, warnings, err := model.buildI2VBody(opts, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(warnings) == 0 {
			t.Error("expected warning for aspectRatio in i2v")
		}
	})
}

func TestBuildMotionControlBody(t *testing.T) {
	cfg := Config{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	prov, _ := New(cfg)
	model := &VideoModel{
		prov:    prov,
		modelID: "kling-v2.6-motion-control",
		mode:    VideoModeMotionControl,
	}

	t.Run("valid motion control request", func(t *testing.T) {
		videoUrl := "https://example.com/video.mp4"
		charOrientation := "image"
		mode := "std"
		duration := 5.0

		opts := &provider.VideoModelV3CallOptions{
			Prompt:      "Dance move",
			AspectRatio: "16:9",
			Duration:    &duration,
			Image: &provider.VideoModelV3File{
				Type: "url",
				URL:  "https://example.com/character.png",
			},
		}

		provOpts := &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
		}

		body, warnings, err := model.buildMotionControlBody(opts, provOpts)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["video_url"] != videoUrl {
			t.Error("video_url not set correctly")
		}

		if body["character_orientation"] != charOrientation {
			t.Error("character_orientation not set correctly")
		}

		if body["mode"] != mode {
			t.Error("mode not set correctly")
		}

		if len(warnings) != 2 {
			t.Errorf("expected 2 warnings for unsupported options, got %d", len(warnings))
		}
	})

	t.Run("missing videoUrl returns error", func(t *testing.T) {
		charOrientation := "image"
		mode := "std"

		provOpts := &ProviderOptions{
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
		}

		_, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, provOpts)
		if err == nil {
			t.Error("expected error for missing videoUrl")
		}
	})

	t.Run("missing characterOrientation returns error", func(t *testing.T) {
		videoUrl := "https://example.com/video.mp4"
		mode := "std"

		provOpts := &ProviderOptions{
			VideoUrl: &videoUrl,
			Mode:     &mode,
		}

		_, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, provOpts)
		if err == nil {
			t.Error("expected error for missing characterOrientation")
		}
	})

	t.Run("missing mode returns error", func(t *testing.T) {
		videoUrl := "https://example.com/video.mp4"
		charOrientation := "image"

		provOpts := &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
		}

		_, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, provOpts)
		if err == nil {
			t.Error("expected error for missing mode")
		}
	})
}

func TestCheckUnsupportedOptions(t *testing.T) {
	cfg := Config{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	prov, _ := New(cfg)
	model := &VideoModel{
		prov:    prov,
		modelID: "kling-v2.6-t2v",
		mode:    VideoModeT2V,
	}

	seed := 123
	fps := 30
	opts := &provider.VideoModelV3CallOptions{
		Resolution: "1920x1080",
		Seed:       &seed,
		FPS:        &fps,
		N:          2,
	}

	warnings := model.checkUnsupportedOptions(opts)

	if len(warnings) != 4 {
		t.Errorf("expected 4 warnings, got %d", len(warnings))
	}
}

func TestExtractProviderOptions(t *testing.T) {
	t.Run("returns empty options when nil", func(t *testing.T) {
		opts, err := extractProviderOptions(nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if opts == nil {
			t.Error("expected non-nil options")
		}
	})

	t.Run("returns empty options when klingai key missing", func(t *testing.T) {
		provOpts := map[string]interface{}{
			"other": "value",
		}

		opts, err := extractProviderOptions(provOpts)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if opts == nil {
			t.Error("expected non-nil options")
		}
	})

	t.Run("extracts klingai options correctly", func(t *testing.T) {
		mode := "pro"
		pollInterval := 3000

		provOpts := map[string]interface{}{
			"klingai": map[string]interface{}{
				"mode":           mode,
				"pollIntervalMs": pollInterval,
			},
		}

		opts, err := extractProviderOptions(provOpts)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if opts.Mode == nil || *opts.Mode != "pro" {
			t.Error("mode not extracted correctly")
		}

		if opts.PollIntervalMs == nil || *opts.PollIntervalMs != 3000 {
			t.Error("pollIntervalMs not extracted correctly")
		}
	})
}

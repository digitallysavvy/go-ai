package klingai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
			name:      "v3.0 text-to-video model",
			modelID:   KlingV3T2V,
			wantMode:  VideoModeT2V,
			wantError: false,
		},
		{
			name:      "v3.0 image-to-video model",
			modelID:   KlingV3I2V,
			wantMode:  VideoModeI2V,
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
		name        string
		modelID     string
		mode        VideoMode
		wantAPIName string
	}{
		{
			name:        "simple t2v model",
			modelID:     "kling-v1-t2v",
			mode:        VideoModeT2V,
			wantAPIName: "kling-v1",
		},
		{
			name:        "versioned t2v model with dots",
			modelID:     "kling-v2.6-t2v",
			mode:        VideoModeT2V,
			wantAPIName: "kling-v2-6",
		},
		{
			name:        "master variant with dots",
			modelID:     "kling-v2.1-master-t2v",
			mode:        VideoModeT2V,
			wantAPIName: "kling-v2-1-master",
		},
		{
			name:        "i2v model",
			modelID:     "kling-v2.6-i2v",
			mode:        VideoModeI2V,
			wantAPIName: "kling-v2-6",
		},
		{
			name:        "motion control model",
			modelID:     "kling-v2.6-motion-control",
			mode:        VideoModeMotionControl,
			wantAPIName: "kling-v2-6",
		},
		{
			name:        "v3.0 t2v model strips .0 suffix",
			modelID:     KlingV3T2V,
			mode:        VideoModeT2V,
			wantAPIName: "kling-v3",
		},
		{
			name:        "v3.0 i2v model strips .0 suffix",
			modelID:     KlingV3I2V,
			mode:        VideoModeI2V,
			wantAPIName: "kling-v3",
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

	t.Run("SpecificationVersion returns v4", func(t *testing.T) {
		if model.SpecificationVersion() != "v4" {
			t.Errorf("expected v4, got %s", model.SpecificationVersion())
		}
	})

	t.Run("Provider returns klingai.video", func(t *testing.T) {
		if model.Provider() != "klingai.video" {
			t.Errorf("expected klingai.video, got %s", model.Provider())
		}
	})

	t.Run("ModelID returns correct ID", func(t *testing.T) {
		if model.ModelID() != "kling-v2.6-t2v" {
			t.Errorf("expected kling-v2.6-t2v, got %s", model.ModelID())
		}
	})

	t.Run("MaxVideosPerCall returns 1", func(t *testing.T) {
		// TS KlingAIVideoModel declares `readonly maxVideosPerCall = 1`.
		max := model.MaxVideosPerCall()
		if max == nil || *max != 1 {
			t.Errorf("expected 1 for MaxVideosPerCall, got %v", max)
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

// TestCheckUnsupportedOptions_ZeroValuesNoWarning mirrors TS's truthy checks
// (`if (options.seed)`, `if (options.fps)`): an explicit zero must not warn,
// matching the falsy behavior of a JS number 0.
func TestCheckUnsupportedOptions_ZeroValuesNoWarning(t *testing.T) {
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

	zeroSeed := 0
	zeroFPS := 0
	opts := &provider.VideoModelV3CallOptions{
		Seed: &zeroSeed,
		FPS:  &zeroFPS,
		N:    1,
	}

	warnings := model.checkUnsupportedOptions(opts)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings for zero seed/fps, got %d: %+v", len(warnings), warnings)
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

func TestIsImageToVideo(t *testing.T) {
	cfg := Config{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	prov, _ := New(cfg)

	tests := []struct {
		name    string
		modelID string
		mode    VideoMode
		want    bool
	}{
		{
			name:    "t2v model returns false",
			modelID: "kling-v2.6-t2v",
			mode:    VideoModeT2V,
			want:    false,
		},
		{
			name:    "i2v model returns true",
			modelID: "kling-v2.6-i2v",
			mode:    VideoModeI2V,
			want:    true,
		},
		{
			name:    "motion-control model returns false",
			modelID: "kling-v2.6-motion-control",
			mode:    VideoModeMotionControl,
			want:    false,
		},
		{
			name:    "v3.0 t2v returns false",
			modelID: KlingV3T2V,
			mode:    VideoModeT2V,
			want:    false,
		},
		{
			name:    "v3.0 i2v returns true",
			modelID: KlingV3I2V,
			mode:    VideoModeI2V,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &VideoModel{
				prov:    prov,
				modelID: tt.modelID,
				mode:    tt.mode,
			}
			if got := model.IsImageToVideo(); got != tt.want {
				t.Errorf("IsImageToVideo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelIDConstants(t *testing.T) {
	// Verify all v3.0 constants have expected string values
	if KlingV3T2V != "kling-v3.0-t2v" {
		t.Errorf("KlingV3T2V = %q, want %q", KlingV3T2V, "kling-v3.0-t2v")
	}
	if KlingV3I2V != "kling-v3.0-i2v" {
		t.Errorf("KlingV3I2V = %q, want %q", KlingV3I2V, "kling-v3.0-i2v")
	}

	// Verify v3.0 constants are accepted by the provider
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("KlingV3T2V accepted as valid model ID", func(t *testing.T) {
		model, err := prov.VideoModel(KlingV3T2V)
		if err != nil {
			t.Errorf("unexpected error for KlingV3T2V: %v", err)
		}
		if model == nil {
			t.Error("expected non-nil model")
		}
	})

	t.Run("KlingV3I2V accepted as valid model ID", func(t *testing.T) {
		model, err := prov.VideoModel(KlingV3I2V)
		if err != nil {
			t.Errorf("unexpected error for KlingV3I2V: %v", err)
		}
		if model == nil {
			t.Error("expected non-nil model")
		}
	})

	t.Run("KlingV3T2V routes to text2video endpoint", func(t *testing.T) {
		vm, err := newVideoModel(prov, KlingV3T2V)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := vm.getEndpoint(); got != "/v1/videos/text2video" {
			t.Errorf("endpoint = %q, want %q", got, "/v1/videos/text2video")
		}
	})

	t.Run("KlingV3I2V routes to image2video endpoint", func(t *testing.T) {
		vm, err := newVideoModel(prov, KlingV3I2V)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := vm.getEndpoint(); got != "/v1/videos/image2video" {
			t.Errorf("endpoint = %q, want %q", got, "/v1/videos/image2video")
		}
	})
}

func TestV3MultiShotOptions(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("multi-shot intelligence mode in T2V", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		multiShot := true
		shotType := "intelligence"

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "A cinematic journey through the seasons",
		}, &ProviderOptions{
			MultiShot: &multiShot,
			ShotType:  &shotType,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["multi_shot"] != true {
			t.Errorf("multi_shot = %v, want true", body["multi_shot"])
		}
		if body["shot_type"] != "intelligence" {
			t.Errorf("shot_type = %v, want intelligence", body["shot_type"])
		}
		if _, ok := body["multi_prompt"]; ok {
			t.Error("multi_prompt should not be set when ShotType is intelligence")
		}
	})

	t.Run("multi-shot customize mode with per-shot prompts in T2V", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		multiShot := true
		shotType := "customize"

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "ignored when multiShot is true",
		}, &ProviderOptions{
			MultiShot: &multiShot,
			ShotType:  &shotType,
			MultiPrompt: []MultiShotPrompt{
				{Index: 1, Prompt: "A lone wolf at dusk", Duration: "3"},
				{Index: 2, Prompt: "The wolf howls at the moon", Duration: "2"},
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["multi_shot"] != true {
			t.Errorf("multi_shot = %v, want true", body["multi_shot"])
		}
		if body["shot_type"] != "customize" {
			t.Errorf("shot_type = %v, want customize", body["shot_type"])
		}

		shots, ok := body["multi_prompt"].([]MultiShotPrompt)
		if !ok {
			t.Fatalf("multi_prompt has wrong type: %T", body["multi_prompt"])
		}
		if len(shots) != 2 {
			t.Errorf("expected 2 shots, got %d", len(shots))
		}
		if shots[0].Index != 1 || shots[0].Prompt != "A lone wolf at dusk" || shots[0].Duration != "3" {
			t.Errorf("shot[0] = %+v, unexpected values", shots[0])
		}
	})

	t.Run("multi-shot in I2V with per-shot prompts", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		multiShot := true
		shotType := "customize"

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/start.png"},
		}, &ProviderOptions{
			MultiShot: &multiShot,
			ShotType:  &shotType,
			MultiPrompt: []MultiShotPrompt{
				{Index: 1, Prompt: "First shot", Duration: "5"},
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["multi_shot"] != true {
			t.Errorf("multi_shot = %v, want true", body["multi_shot"])
		}
		if body["shot_type"] != "customize" {
			t.Errorf("shot_type = %v, want customize", body["shot_type"])
		}
		if body["multi_prompt"] == nil {
			t.Error("multi_prompt should be set")
		}
	})
}

func TestV3VoiceControl(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("voice list in T2V body", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		sound := "on"

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "<<<voice_1>>> says hello world",
		}, &ProviderOptions{
			Sound:     &sound,
			VoiceList: []VoiceRef{{VoiceID: "voice-abc-123"}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		voices, ok := body["voice_list"].([]VoiceRef)
		if !ok {
			t.Fatalf("voice_list has wrong type: %T", body["voice_list"])
		}
		if len(voices) != 1 || voices[0].VoiceID != "voice-abc-123" {
			t.Errorf("voice_list = %+v, unexpected values", voices)
		}
	})

	t.Run("voice list in I2V body", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		sound := "on"

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "<<<voice_1>>> narrates the scene",
			Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{
			Sound:     &sound,
			VoiceList: []VoiceRef{{VoiceID: "voice-xyz-456"}, {VoiceID: "voice-xyz-789"}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		voices, ok := body["voice_list"].([]VoiceRef)
		if !ok {
			t.Fatalf("voice_list has wrong type: %T", body["voice_list"])
		}
		if len(voices) != 2 {
			t.Errorf("expected 2 voices, got %d", len(voices))
		}
	})

	t.Run("empty voice list does not set field", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "test",
		}, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if _, ok := body["voice_list"]; ok {
			t.Error("voice_list should not be set when VoiceList is empty")
		}
	})
}

func TestV3ElementControl(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("element list in I2V body", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "Character performs action",
			Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{
			ElementList: []ElementRef{{ElementID: 101}, {ElementID: 202}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		elements, ok := body["element_list"].([]ElementRef)
		if !ok {
			t.Fatalf("element_list has wrong type: %T", body["element_list"])
		}
		if len(elements) != 2 {
			t.Errorf("expected 2 elements, got %d", len(elements))
		}
		if elements[0].ElementID != 101 || elements[1].ElementID != 202 {
			t.Errorf("element_list = %+v, unexpected values", elements)
		}
	})

	t.Run("element list not set in T2V (T2V has no element support)", func(t *testing.T) {
		// ElementList is an I2V-only feature; T2V body builder does not include it.
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "test",
		}, &ProviderOptions{
			ElementList: []ElementRef{{ElementID: 101}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if _, ok := body["element_list"]; ok {
			t.Error("element_list should not be in T2V body")
		}
	})

	t.Run("empty element list does not set field", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if _, ok := body["element_list"]; ok {
			t.Error("element_list should not be set when ElementList is empty")
		}
	})
}

// TestKlingMotionControlModelIDRouting verifies that v3.0 motion-control model IDs detect the
// correct mode and route to the /v1/videos/motion-control endpoint.

// TestKlingMotionControlModelIDRouting verifies that the v3.0 motion-control model ID
// detects the correct mode and routes to /v1/videos/motion-control.
func TestKlingMotionControlModelIDRouting(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("KlingV3MotionControl detects motion-control mode", func(t *testing.T) {
		vm, err := newVideoModel(prov, KlingV3MotionControl)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vm.mode != VideoModeMotionControl {
			t.Errorf("mode = %q, want %q", vm.mode, VideoModeMotionControl)
		}
		if got := vm.getEndpoint(); got != "/v1/videos/motion-control" {
			t.Errorf("endpoint = %q, want %q", got, "/v1/videos/motion-control")
		}
	})

	t.Run("KlingV3MotionControl accepted by provider", func(t *testing.T) {
		model, err := prov.VideoModel(KlingV3MotionControl)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if model == nil {
			t.Error("expected non-nil model")
		}
	})

	t.Run("KlingV3MotionControl API model name strips .0 suffix", func(t *testing.T) {
		vm, _ := newVideoModel(prov, KlingV3MotionControl)
		if got := vm.getAPIModelName(); got != "kling-v3" {
			t.Errorf("getAPIModelName() = %q, want %q", got, "kling-v3")
		}
	})

	t.Run("missing videoUrl returns ErrCodeKlingVideoMissingOptions", func(t *testing.T) {
		vm, _ := newVideoModel(prov, KlingV3MotionControl)
		charOrientation := "image"
		mode := "std"

		_, _, err := vm.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
		})
		if err == nil {
			t.Fatal("expected error for missing videoUrl")
		}
		kErr, ok := err.(*Error)
		if !ok {
			t.Fatalf("expected *Error, got %T: %v", err, err)
		}
		if kErr.ErrorCode != ErrCodeKlingVideoMissingOptions {
			t.Errorf("ErrorCode = %q, want %q", kErr.ErrorCode, ErrCodeKlingVideoMissingOptions)
		}
	})

	t.Run("missing characterOrientation returns ErrCodeKlingVideoMissingOptions", func(t *testing.T) {
		vm, _ := newVideoModel(prov, KlingV3MotionControl)
		videoUrl := "https://example.com/ref.mp4"
		mode := "std"

		_, _, err := vm.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			VideoUrl: &videoUrl,
			Mode:     &mode,
		})
		if err == nil {
			t.Fatal("expected error for missing characterOrientation")
		}
		kErr, ok := err.(*Error)
		if !ok || kErr.ErrorCode != ErrCodeKlingVideoMissingOptions {
			t.Errorf("expected ErrCodeKlingVideoMissingOptions, got %v", err)
		}
	})

	t.Run("missing mode returns ErrCodeKlingVideoMissingOptions", func(t *testing.T) {
		vm, _ := newVideoModel(prov, KlingV3MotionControl)
		videoUrl := "https://example.com/ref.mp4"
		charOrientation := "image"

		_, _, err := vm.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
		})
		if err == nil {
			t.Fatal("expected error for missing mode")
		}
		kErr, ok := err.(*Error)
		if !ok || kErr.ErrorCode != ErrCodeKlingVideoMissingOptions {
			t.Errorf("expected ErrCodeKlingVideoMissingOptions, got %v", err)
		}
	})
}

// TestKlingMultiShotSerialization verifies that MultiShotPrompt.Duration is a string and
// that multi-shot fields (multi_shot, shot_type, multi_prompt) serialize correctly.
func TestKlingMultiShotSerialization(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("MultiShotPrompt.Duration is string type not int", func(t *testing.T) {
		shot := MultiShotPrompt{Index: 1, Prompt: "A scene", Duration: "5"}
		if shot.Duration != "5" {
			t.Errorf("Duration = %q, want string '5'", shot.Duration)
		}
	})

	t.Run("multi-shot serializes in T2V body", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		multiShot := true
		shotType := "customize"

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "A cinematic journey",
		}, &ProviderOptions{
			MultiShot: &multiShot,
			ShotType:  &shotType,
			MultiPrompt: []MultiShotPrompt{
				{Index: 1, Prompt: "Opening", Duration: "3"},
				{Index: 2, Prompt: "Climax", Duration: "2"},
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if body["multi_shot"] != true {
			t.Errorf("multi_shot = %v, want true", body["multi_shot"])
		}
		if body["shot_type"] != "customize" {
			t.Errorf("shot_type = %v, want customize", body["shot_type"])
		}
		shots, ok := body["multi_prompt"].([]MultiShotPrompt)
		if !ok {
			t.Fatalf("multi_prompt has wrong type: %T", body["multi_prompt"])
		}
		if len(shots) != 2 {
			t.Errorf("expected 2 shots, got %d", len(shots))
		}
		if shots[0].Duration != "3" {
			t.Errorf("shots[0].Duration = %q, want '3' (string)", shots[0].Duration)
		}
	})

	t.Run("multi-shot serializes in I2V body", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		multiShot := true
		shotType := "intelligence"

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/start.png"},
		}, &ProviderOptions{
			MultiShot: &multiShot,
			ShotType:  &shotType,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if body["multi_shot"] != true {
			t.Errorf("multi_shot = %v, want true", body["multi_shot"])
		}
		if body["shot_type"] != "intelligence" {
			t.Errorf("shot_type = %v, want intelligence", body["shot_type"])
		}
	})
}

// TestKlingElementControlSerialization verifies that ElementRef serializes correctly
// to the element_list body key in I2V and motion-control modes.
func TestKlingElementControlSerialization(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("ElementList serializes to element_list in I2V", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{
			ElementList: []ElementRef{{ElementID: 101}, {ElementID: 202}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		elements, ok := body["element_list"].([]ElementRef)
		if !ok {
			t.Fatalf("element_list has wrong type: %T", body["element_list"])
		}
		if len(elements) != 2 || elements[0].ElementID != 101 || elements[1].ElementID != 202 {
			t.Errorf("element_list = %+v, unexpected values", elements)
		}
	})

	t.Run("empty ElementList does not set element_list", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := body["element_list"]; ok {
			t.Error("element_list should not be set when ElementList is empty")
		}
	})

	t.Run("ElementList serializes to element_list in motion-control", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3MotionControl, mode: VideoModeMotionControl}
		videoUrl := "https://example.com/ref.mp4"
		charOrientation := "image"
		mode := "std"

		body, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
			ElementList:          []ElementRef{{ElementID: 55}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		elements, ok := body["element_list"].([]ElementRef)
		if !ok {
			t.Fatalf("element_list has wrong type: %T", body["element_list"])
		}
		if len(elements) != 1 || elements[0].ElementID != 55 {
			t.Errorf("element_list = %+v, unexpected values", elements)
		}
	})
}

// TestKlingMotionControlElementListMaxOne verifies that providing more than 1 element
// in motion-control mode returns an error (TS SDK comment: "Motion Control: 1").
func TestKlingMotionControlElementListMaxOne(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	videoUrl := "https://example.com/video.mp4"
	charOrientation := "image"
	mode := "std"

	t.Run("single element in motion-control succeeds", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3MotionControl, mode: VideoModeMotionControl}

		_, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
			ElementList:          []ElementRef{{ElementID: 1}},
		})
		if err != nil {
			t.Errorf("expected no error for 1 element, got %v", err)
		}
	})

	t.Run("two elements in motion-control passes through to API", func(t *testing.T) {
		// TS SDK does not validate element_list count client-side; it passes through
		// to the API which enforces the max-1 constraint server-side.
		model := &VideoModel{prov: prov, modelID: KlingV3MotionControl, mode: VideoModeMotionControl}

		body, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
			ElementList:          []ElementRef{{ElementID: 1}, {ElementID: 2}},
		})
		if err != nil {
			t.Errorf("expected no error (passthrough to API), got %v", err)
		}
		list, ok := body["element_list"]
		if !ok {
			t.Error("expected element_list in body")
		}
		elems, _ := list.([]ElementRef)
		if len(elems) != 2 {
			t.Errorf("expected 2 elements passed through, got %d", len(elems))
		}
	})

	t.Run("zero elements in motion-control succeeds", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3MotionControl, mode: VideoModeMotionControl}

		_, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
			VideoUrl:             &videoUrl,
			CharacterOrientation: &charOrientation,
			Mode:                 &mode,
		})
		if err != nil {
			t.Errorf("expected no error for no elements, got %v", err)
		}
	})
}

// TestKlingVoiceControlSerialization verifies that VoiceRef serializes correctly
// to the voice_list body key in T2V and I2V modes.
func TestKlingVoiceControlSerialization(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("VoiceRef serializes to voice_list in T2V", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		sound := "on"

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "<<<voice_1>>> says hello",
		}, &ProviderOptions{
			Sound:     &sound,
			VoiceList: []VoiceRef{{VoiceID: "voice-abc-123"}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		voices, ok := body["voice_list"].([]VoiceRef)
		if !ok {
			t.Fatalf("voice_list has wrong type: %T", body["voice_list"])
		}
		if len(voices) != 1 || voices[0].VoiceID != "voice-abc-123" {
			t.Errorf("voice_list = %+v, unexpected values", voices)
		}
	})

	t.Run("VoiceRef serializes to voice_list in I2V", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		sound := "on"

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "<<<voice_1>>> narrates",
			Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{
			Sound:     &sound,
			VoiceList: []VoiceRef{{VoiceID: "v1"}, {VoiceID: "v2"}},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		voices, ok := body["voice_list"].([]VoiceRef)
		if !ok {
			t.Fatalf("voice_list has wrong type: %T", body["voice_list"])
		}
		if len(voices) != 2 {
			t.Errorf("expected 2 voices, got %d", len(voices))
		}
	})

	t.Run("empty VoiceList does not set field", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}

		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{
			Prompt: "test",
		}, &ProviderOptions{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := body["voice_list"]; ok {
			t.Error("voice_list should not be set when VoiceList is empty")
		}
	})
}

// TestKlingMotionBrushSerialization verifies that StaticMask and DynamicMasks serialize
// correctly using the wire keys static_mask and dynamic_masks, with float64 Trajectory
// coordinates (matching TS number type).
func TestKlingMotionBrushSerialization(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	t.Run("StaticMask serializes as static_mask in I2V", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		mask := "https://mask.com/static.png"

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{
			StaticMask: &mask,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if body["static_mask"] != mask {
			t.Errorf("static_mask = %v, want %q", body["static_mask"], mask)
		}
		// Confirm wrong key not present
		if _, ok := body["static_brush_mask"]; ok {
			t.Error("static_brush_mask should not exist; key is static_mask")
		}
	})

	t.Run("DynamicMasks serialize as dynamic_masks with float64 trajectories", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}

		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{
			Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		}, &ProviderOptions{
			DynamicMasks: []DynamicMask{
				{
					Mask: "https://mask.com/dyn.png",
					Trajectories: []Trajectory{
						{X: 100.0, Y: 200.0},
						{X: 150.5, Y: 250.5},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		masks, ok := body["dynamic_masks"].([]DynamicMask)
		if !ok {
			t.Fatalf("dynamic_masks has wrong type: %T", body["dynamic_masks"])
		}
		if len(masks) != 1 || masks[0].Mask != "https://mask.com/dyn.png" {
			t.Errorf("dynamic_masks = %+v", masks)
		}
		if len(masks[0].Trajectories) != 2 {
			t.Errorf("expected 2 trajectories, got %d", len(masks[0].Trajectories))
		}
		// float64 coordinates matching TS number type
		if masks[0].Trajectories[0].X != 100.0 || masks[0].Trajectories[0].Y != 200.0 {
			t.Errorf("trajectory[0] = %+v, want X=100.0 Y=200.0", masks[0].Trajectories[0])
		}
		// Confirm wrong key not present
		if _, ok := body["dynamic_brushes"]; ok {
			t.Error("dynamic_brushes should not exist; key is dynamic_masks")
		}
	})

	t.Run("Trajectory X Y are float64 matching TS number type", func(t *testing.T) {
		traj := Trajectory{X: 42.5, Y: 84.25}
		x, y := traj.X, traj.Y
		if x != 42.5 || y != 84.25 {
			t.Errorf("trajectory = %+v", traj)
		}
	})
}

// TestKlingV30MotionControlFullRequest verifies that a full motion-control request
// serializes all TS-supported fields correctly.
// Per TS SDK: motion-control supports video_url, character_orientation, mode, prompt,
// image_url, keep_original_sound, watermark_info, element_list (max 1).
// It does NOT include multi_shot or voice_list.
func TestKlingV30MotionControlFullRequest(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)
	model := &VideoModel{prov: prov, modelID: KlingV3MotionControl, mode: VideoModeMotionControl}

	videoUrl := "https://example.com/reference.mp4"
	charOrientation := "image"
	mode := "std"
	keepSound := "yes"
	watermark := true

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "Perform the dance move",
		Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/character.png"},
	}

	provOpts := &ProviderOptions{
		VideoUrl:             &videoUrl,
		CharacterOrientation: &charOrientation,
		Mode:                 &mode,
		KeepOriginalSound:    &keepSound,
		WatermarkEnabled:     &watermark,
		ElementList:          []ElementRef{{ElementID: 42}},
	}

	body, warnings, err := model.buildMotionControlBody(opts, provOpts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Required fields
	if body["video_url"] != videoUrl {
		t.Errorf("video_url = %v, want %q", body["video_url"], videoUrl)
	}
	if body["character_orientation"] != charOrientation {
		t.Errorf("character_orientation = %v, want %q", body["character_orientation"], charOrientation)
	}
	if body["mode"] != mode {
		t.Errorf("mode = %v, want %q", body["mode"], mode)
	}

	// Optional fields
	if body["prompt"] != "Perform the dance move" {
		t.Errorf("prompt = %v", body["prompt"])
	}
	if body["image_url"] != "https://example.com/character.png" {
		t.Errorf("image_url = %v", body["image_url"])
	}
	if body["keep_original_sound"] != "yes" {
		t.Errorf("keep_original_sound = %v", body["keep_original_sound"])
	}
	watermarkInfo, ok := body["watermark_info"].(map[string]bool)
	if !ok || !watermarkInfo["enabled"] {
		t.Errorf("watermark_info = %v, want {enabled: true}", body["watermark_info"])
	}

	// Element control
	elements, ok := body["element_list"].([]ElementRef)
	if !ok || len(elements) != 1 || elements[0].ElementID != 42 {
		t.Errorf("element_list = %v (type %T)", body["element_list"], body["element_list"])
	}

	// Motion-control must NOT include multi-shot or voice fields (per TS SDK)
	if _, exists := body["multi_shot"]; exists {
		t.Error("motion-control body must not contain multi_shot")
	}
	if _, exists := body["voice_list"]; exists {
		t.Error("motion-control body must not contain voice_list")
	}

	// No warnings
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d: %v", len(warnings), warnings)
	}
}

// TestIntegrationV3Models verifies v3.0 model initialization against the live API.
// Skipped when KLINGAI_ACCESS_KEY or KLINGAI_SECRET_KEY are not set.
func TestIntegrationV3Models(t *testing.T) {
	if os.Getenv("KLINGAI_ACCESS_KEY") == "" || os.Getenv("KLINGAI_SECRET_KEY") == "" {
		t.Skip("Skipping: KLINGAI_ACCESS_KEY or KLINGAI_SECRET_KEY not configured")
	}

	prov, err := New(Config{})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	t.Run("KlingV3T2V model initializes successfully", func(t *testing.T) {
		model, err := prov.VideoModel(KlingV3T2V)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if model.ModelID() != KlingV3T2V {
			t.Errorf("ModelID() = %q, want %q", model.ModelID(), KlingV3T2V)
		}
	})

	t.Run("KlingV3I2V model initializes successfully", func(t *testing.T) {
		model, err := prov.VideoModel(KlingV3I2V)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if model.ModelID() != KlingV3I2V {
			t.Errorf("ModelID() = %q, want %q", model.ModelID(), KlingV3I2V)
		}
		if !model.(*VideoModel).IsImageToVideo() {
			t.Error("IsImageToVideo() should return true for KlingV3I2V")
		}
	})
}

// TestKlingMotionControlModelName verifies that model_name is included in the
// motion-control request body (CRITICAL: missing model_name causes API errors).
func TestKlingMotionControlModelName(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)
	model := &VideoModel{prov: prov, modelID: KlingV3MotionControl, mode: VideoModeMotionControl}

	videoUrl := "https://example.com/ref.mp4"
	orientation := "image"
	mode := "std"

	body, _, err := model.buildMotionControlBody(&provider.VideoModelV3CallOptions{}, &ProviderOptions{
		VideoUrl:             &videoUrl,
		CharacterOrientation: &orientation,
		Mode:                 &mode,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if body["model_name"] != "kling-v3" {
		t.Errorf("model_name = %v, want %q", body["model_name"], "kling-v3")
	}
}

// TestKlingWatermarkInfoT2VAndI2V verifies that watermark_info is serialized in T2V and I2V
// request bodies when WatermarkEnabled is set, matching TS SDK behavior.
func TestKlingWatermarkInfoT2VAndI2V(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)
	watermark := true
	opts := &ProviderOptions{WatermarkEnabled: &watermark}

	t.Run("T2V includes watermark_info", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{}, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		info, ok := body["watermark_info"].(map[string]bool)
		if !ok || !info["enabled"] {
			t.Errorf("watermark_info = %v, want {enabled: true}", body["watermark_info"])
		}
	})

	t.Run("I2V includes watermark_info", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		body, _, err := model.buildI2VBody(&provider.VideoModelV3CallOptions{}, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		info, ok := body["watermark_info"].(map[string]bool)
		if !ok || !info["enabled"] {
			t.Errorf("watermark_info = %v, want {enabled: true}", body["watermark_info"])
		}
	})
}

// TestKlingDurationFormat verifies that duration is serialized with strconv.FormatFloat
// (preserving decimal precision) rather than rounded integer format.
func TestKlingDurationFormat(t *testing.T) {
	cfg := Config{AccessKey: "test-ak", SecretKey: "test-sk"}
	prov, _ := New(cfg)

	dur := 5.0
	opts := &provider.VideoModelV3CallOptions{Duration: &dur}
	provOpts := &ProviderOptions{}

	t.Run("T2V integer duration", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3T2V, mode: VideoModeT2V}
		body, _, err := model.buildT2VBody(opts, provOpts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["duration"] != "5" {
			t.Errorf("duration = %q, want %q", body["duration"], "5")
		}
	})

	t.Run("I2V integer duration", func(t *testing.T) {
		model := &VideoModel{prov: prov, modelID: KlingV3I2V, mode: VideoModeI2V}
		body, _, err := model.buildI2VBody(opts, provOpts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["duration"] != "5" {
			t.Errorf("duration = %q, want %q", body["duration"], "5")
		}
	})
}

// TestKlingPassthroughOptions verifies that unknown provider option keys are forwarded
// to the API request body, matching TS SDK addPassthroughOptions behavior.
func TestKlingPassthroughOptions(t *testing.T) {
	rawOpts := map[string]interface{}{
		"klingai": map[string]interface{}{
			"mode":         "std",
			"custom_field": "custom_value",
			"another_key":  42,
		},
	}

	provOpts, err := extractProviderOptions(rawOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provOpts.Mode == nil || *provOpts.Mode != "std" {
		t.Errorf("Mode = %v, want %q", provOpts.Mode, "std")
	}

	if provOpts.Additional["custom_field"] != "custom_value" {
		t.Errorf("Additional[custom_field] = %v, want %q", provOpts.Additional["custom_field"], "custom_value")
	}
	if provOpts.Additional["another_key"] == nil {
		t.Error("Additional[another_key] should be set")
	}
	// Known keys must NOT appear in Additional
	if _, exists := provOpts.Additional["mode"]; exists {
		t.Error("known key 'mode' must not appear in Additional")
	}
}

// ─── frameImages / inputReferences (mi2v) / async doStart-doStatus / SSRF ──────

func newKlingAITestModel(t *testing.T, modelID string, handler http.HandlerFunc) (*VideoModel, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	prov, err := New(Config{AccessKey: "ak", SecretKey: "sk", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model, err := newVideoModel(prov, modelID)
	if err != nil {
		t.Fatalf("newVideoModel() error: %v", err)
	}
	return model, server
}

// TestBuildRequestBody_FrameImages verifies the v4 frameImages call option
// (first_frame/last_frame) resolves the same way as the legacy image/
// imageTail fields for image-to-video (TS resolveStartImage/resolveImageTail).
func TestBuildRequestBody_FrameImages(t *testing.T) {
	model := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-i2v", mode: VideoModeI2V}

	body, warnings, endpointPath, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{
		Prompt: "test",
		FrameImages: []provider.VideoFrameImage{
			{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.jpg"}},
			{FrameType: provider.VideoFrameTypeLastFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/last.jpg"}},
		},
	}, &ProviderOptions{})
	if err != nil {
		t.Fatalf("buildRequestBody() error: %v", err)
	}
	if endpointPath != "/v1/videos/image2video" {
		t.Errorf("endpointPath = %q", endpointPath)
	}
	if body["image"] != "https://example.com/first.jpg" {
		t.Errorf("image = %v", body["image"])
	}
	if body["image_tail"] != "https://example.com/last.jpg" {
		t.Errorf("image_tail = %v", body["image_tail"])
	}
	for _, w := range warnings {
		if w.Feature == "frameImages" {
			t.Errorf("unexpected frameImages warning: %+v", w)
		}
	}
}

// TestBuildRequestBody_FrameImagesRejectsVideo mirrors TS "KlingAI does not
// accept video as a frame image".
func TestBuildRequestBody_FrameImagesRejectsVideo(t *testing.T) {
	model := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-i2v", mode: VideoModeI2V}

	body, warnings, _, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{
		FrameImages: []provider.VideoFrameImage{
			{FrameType: provider.VideoFrameTypeFirstFrame, Image: provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.mp4", MediaType: "video/mp4"}},
		},
	}, &ProviderOptions{})
	if err != nil {
		t.Fatalf("buildRequestBody() error: %v", err)
	}
	if _, exists := body["image"]; exists {
		t.Errorf("expected no image field, got %v", body["image"])
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "frameImages" {
			found = true
		}
	}
	if !found {
		t.Error("expected a frameImages warning rejecting the video file")
	}
}

// TestBuildRequestBody_InputReferences_ElevatesToMi2V mirrors TS "wan3
// (all-in-one)"-style effective-mode elevation: an i2v model given
// inputReferences (and no frameImages) is routed to multi-image2video.
func TestBuildRequestBody_InputReferences_ElevatesToMi2V(t *testing.T) {
	model := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-i2v", mode: VideoModeI2V}

	body, warnings, endpointPath, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{
		Prompt: "test",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/ref1.jpg"},
			{Type: "url", URL: "https://example.com/ref2.jpg"},
		},
	}, &ProviderOptions{})
	if err != nil {
		t.Fatalf("buildRequestBody() error: %v", err)
	}
	if endpointPath != "/v1/videos/multi-image2video" {
		t.Errorf("endpointPath = %q, want multi-image2video", endpointPath)
	}
	imageList, ok := body["image_list"].([]map[string]interface{})
	if !ok || len(imageList) != 2 {
		t.Fatalf("image_list = %#v", body["image_list"])
	}
	if imageList[0]["image"] != "https://example.com/ref1.jpg" {
		t.Errorf("image_list[0] = %v", imageList[0])
	}
	for _, w := range warnings {
		if w.Feature == "inputReferences" {
			t.Errorf("unexpected inputReferences warning: %+v", w)
		}
	}
}

// TestBuildRequestBody_InputReferences_UnsupportedOnT2V mirrors TS's warning
// when inputReferences are supplied to a non-image-to-video model.
func TestBuildRequestBody_InputReferences_UnsupportedOnT2V(t *testing.T) {
	model := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-t2v", mode: VideoModeT2V}

	_, warnings, endpointPath, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{
		Prompt:          "test",
		InputReferences: []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/ref.jpg"}},
	}, &ProviderOptions{})
	if err != nil {
		t.Fatalf("buildRequestBody() error: %v", err)
	}
	if endpointPath != "/v1/videos/text2video" {
		t.Errorf("endpointPath = %q, want text2video", endpointPath)
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "inputReferences" {
			found = true
		}
	}
	if !found {
		t.Error("expected an inputReferences unsupported warning for t2v")
	}
}

// TestBuildRequestBody_GenerateAudioOverridesSound verifies the top-level
// generateAudio call option maps to sound "on"/"off" and takes precedence
// over providerOptions.klingai.sound.
func TestBuildRequestBody_GenerateAudioOverridesSound(t *testing.T) {
	model := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-t2v", mode: VideoModeT2V}
	generateAudio := true
	sound := "off"

	body, _, err := model.buildT2VBody(&provider.VideoModelV3CallOptions{GenerateAudio: &generateAudio}, &ProviderOptions{Sound: &sound})
	if err != nil {
		t.Fatalf("buildT2VBody() error: %v", err)
	}
	if body["sound"] != "on" {
		t.Errorf("sound = %v, want on", body["sound"])
	}
}

// TestDoStart_DoStatus_RoundTrip exercises the provider.VideoModelStarter and
// provider.VideoModelStatusChecker interfaces directly, independent of
// DoGenerate's synthesized polling loop.
func TestDoStart_DoStatus_RoundTrip(t *testing.T) {
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos/text2video":
			if r.Header.Get("Authorization") == "" {
				t.Errorf("expected Authorization header on doStart")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "message": "ok",
				"data": map[string]interface{}{"task_id": "task-async", "task_status": "submitted"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/videos/text2video/task-async":
			if r.Header.Get("Authorization") == "" {
				t.Errorf("expected Authorization header on doStatus")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "message": "ok",
				"data": map[string]interface{}{
					"task_id": "task-async", "task_status": "succeed",
					"task_result": map[string]interface{}{
						"videos": []map[string]interface{}{{"id": "v1", "url": "https://cdn.example.com/video.mp4"}},
					},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	defer server.Close()

	var starter provider.VideoModelStarter = model
	var checker provider.VideoModelStatusChecker = model

	startResult, err := starter.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "test", N: 1},
	})
	if err != nil {
		t.Fatalf("DoStart() error: %v", err)
	}
	var op klingaiOperation
	if err := json.Unmarshal(startResult.Operation, &op); err != nil || op.TaskID != "task-async" || op.EndpointPath != "/v1/videos/text2video" {
		t.Fatalf("operation = %s", startResult.Operation)
	}

	status, err := checker.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error: %v", err)
	}
	if status.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("status = %v", status.Status)
	}
	if len(status.Videos) != 1 || status.Videos[0].URL != "https://cdn.example.com/video.mp4" {
		t.Errorf("videos = %+v", status.Videos)
	}
}

// TestDoGenerate_ComposesStartAndStatus verifies DoGenerate is built
// entirely on top of DoStart+DoStatus, polling until the task succeeds.
func TestDoGenerate_ComposesStartAndStatus(t *testing.T) {
	pollCount := 0
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/videos/text2video":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "message": "ok",
				"data": map[string]interface{}{"task_id": "task-1", "task_status": "submitted"},
			})
		case r.URL.Path == "/v1/videos/text2video/task-1":
			pollCount++
			if pollCount < 2 {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"code": 0, "message": "ok",
					"data": map[string]interface{}{"task_id": "task-1", "task_status": "processing"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "message": "ok",
				"data": map[string]interface{}{
					"task_id": "task-1", "task_status": "succeed",
					"task_result": map[string]interface{}{
						"videos": []map[string]interface{}{{"id": "v1", "url": "https://cdn.example.com/video.mp4"}},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test", N: 1,
		ProviderOptions: map[string]interface{}{"klingai": map[string]interface{}{"pollIntervalMs": 5}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if len(result.Videos) != 1 || result.Videos[0].URL != "https://cdn.example.com/video.mp4" {
		t.Fatalf("videos = %+v", result.Videos)
	}
	if pollCount < 2 {
		t.Errorf("expected at least 2 status polls, got %d", pollCount)
	}
}

// TestDoGenerate_TaskFailed mirrors TS's task_status "failed" handling,
// propagated through DoStatus's error status into a KLINGAI_VIDEO_GENERATION_FAILED error.
func TestDoGenerate_TaskFailed(t *testing.T) {
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/videos/text2video":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "message": "ok",
				"data": map[string]interface{}{"task_id": "task-fail", "task_status": "submitted"},
			})
		case r.URL.Path == "/v1/videos/text2video/task-fail":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "message": "ok",
				"data": map[string]interface{}{"task_id": "task-fail", "task_status": "failed", "task_status_msg": "content moderation"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test", N: 1,
		ProviderOptions: map[string]interface{}{"klingai": map[string]interface{}{"pollIntervalMs": 5}},
	})
	if err == nil {
		t.Fatal("expected an error when the task fails")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.ErrorCode != "KLINGAI_VIDEO_GENERATION_FAILED" {
		t.Errorf("ErrorCode = %q", apiErr.ErrorCode)
	}
	if !strings.Contains(err.Error(), "content moderation") {
		t.Errorf("expected failure detail in error, got: %v", err)
	}
}

// TestDoStatus_RejectsPrivateIPRedirect verifies the SSRF fix (a580ec8):
// DoStatus polls through fileutil's validated-redirect client, so a
// redirect from the (trusted) provider origin to a private/link-local
// address is rejected instead of followed.
func TestDoStatus_RejectsPrivateIPRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/videos/text2video/redirect-task", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prov, err := New(Config{AccessKey: "ak", SecretKey: "sk", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model, err := newVideoModel(prov, "kling-v2.6-t2v")
	if err != nil {
		t.Fatalf("newVideoModel() error: %v", err)
	}

	op, _ := json.Marshal(klingaiOperation{TaskID: "redirect-task", EndpointPath: "/v1/videos/text2video"})
	_, err = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil {
		t.Fatal("expected an error rejecting the private-IP redirect target")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected an SSRF validation error, got: %v", err)
	}
}

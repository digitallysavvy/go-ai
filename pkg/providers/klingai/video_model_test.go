package klingai

import (
	"os"
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

package klingai

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestDetectModeIsNoSuchModelError(t *testing.T) {
	_, err := detectMode("kling-v2.6-invalid")
	var nsm *providererrors.NoSuchModelError
	if !errors.As(err, &nsm) || nsm.ModelID != "kling-v2.6-invalid" || nsm.ModelType != "videoModel" {
		t.Fatalf("err = %v", err)
	}
}

// A cancelled context propagates unchanged (TS lets the abort through).
func TestDoStartCancelledContextIsNotWrapped(t *testing.T) {
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := model.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TS `!= null` checks keep empty arrays; `if (options.duration)` skips 0.
func TestEmptyArrayOptionsAreSentAndZeroDurationDoesNotWarn(t *testing.T) {
	model := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-t2v", mode: VideoModeT2V}
	provOpts, err := extractProviderOptions(map[string]interface{}{"klingai": map[string]interface{}{
		"multiPrompt": []interface{}{}, "voiceList": []interface{}{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body, _, _, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{Prompt: "x"}, provOpts)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"multi_prompt", "voice_list"} {
		if v, ok := body[k]; !ok || v == nil {
			t.Errorf("%s missing from body %v", k, body)
		}
	}

	mc := &VideoModel{prov: &Provider{}, modelID: "kling-v2.6-motion-control", mode: VideoModeMotionControl}
	zero := 0.0
	url, orient, mode := "https://v", "image", "std"
	_, warns, _, err := mc.buildRequestBody(&provider.VideoModelV3CallOptions{Duration: &zero},
		&ProviderOptions{VideoUrl: &url, CharacterOrientation: &orient, Mode: &mode})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warns {
		if w.Feature == "duration" {
			t.Errorf("unexpected duration warning for 0")
		}
	}
}

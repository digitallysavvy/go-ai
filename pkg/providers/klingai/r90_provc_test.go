package klingai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provcAISDKErrorTextIsTSMessage: TS throws AISDKError({name, message})
// for a motion-control call without its required options
// (klingai-video-model.ts 720-729) and for responses without a task id or
// videos (240-246, 365-371, 393-398), so the error text is the message
// alone. Go prefixed "klingai error (<NAME>): ". ErrorCode and the *Error
// type are unchanged.
func TestR90provcAISDKErrorTextIsTSMessage(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := newVideoModel(p, "kling-v2.6-motion-control")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	var ke *Error
	want := "KlingAI Motion Control requires providerOptions.klingai with videoUrl, characterOrientation, and mode."
	if !errors.As(err, &ke) || ke.ErrorCode != ErrCodeKlingVideoMissingOptions || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if got := newVideoGenerationErrorText("No valid video URLs in response").Error(); got != "No valid video URLs in response" {
		t.Fatalf("generation error text = %q", got)
	}
}

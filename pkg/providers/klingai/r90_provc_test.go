package klingai

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of klingaiErrorDataSchema ({code, message}, undeclared keys
// stripped) for a matching body and undefined otherwise.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(parseKlingAIErrorBody(429, nil, []byte(body)), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	if got := data(`{"code":1302,"message":"slow","request_id":"r"}`); !reflect.DeepEqual(got, map[string]interface{}{"code": float64(1302), "message": "slow"}) {
		t.Errorf("matched data = %#v", got)
	}
	if got := data(`{"error":"x"}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}

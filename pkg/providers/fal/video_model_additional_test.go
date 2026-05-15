package fal

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestFalVideoModelPollOptionsAndMaxVideos(t *testing.T) {
	model := NewVideoModel(&Provider{}, "luma-ray")
	if model.MaxVideosPerCall() != nil {
		t.Fatalf("MaxVideosPerCall() = %v, want nil", model.MaxVideosPerCall())
	}

	opts := model.getPollOptions(map[string]interface{}{
		"fal": map[string]interface{}{
			"pollIntervalMs": 10,
			"pollTimeoutMs":  20,
		},
	})
	if opts.PollIntervalMs != 10 || opts.PollTimeoutMs != 20 {
		t.Fatalf("getPollOptions() = %#v", opts)
	}
}

func TestFalVideoModelConvertImmediateAndPolledErrors(t *testing.T) {
	model := NewVideoModel(&Provider{}, "luma-ray")

	if _, err := model.convertImmediateResponse(context.Background(), falSubmitResponse{}); err == nil {
		t.Fatal("convertImmediateResponse(nil video) should fail")
	}

	resp, err := model.convertImmediateResponse(context.Background(), falSubmitResponse{
		RequestID: "r1",
		Video:     &falVideoResult{URL: "https://example.com/video.mp4"},
	})
	if err != nil {
		t.Fatalf("convertImmediateResponse(valid) error = %v", err)
	}
	if len(resp.Videos) != 1 || resp.Videos[0].URL == "" {
		t.Fatalf("convertImmediateResponse(valid) videos = %#v", resp.Videos)
	}

	if _, err := model.convertPolledResponse(context.Background(), &falVideoResponse{}); err == nil {
		t.Fatal("convertPolledResponse(nil URL) should fail")
	}

	// Download from an invalid URL should trigger warning fallback.
	resp, err = model.convertPolledResponse(context.Background(), &falVideoResponse{
		Video: &falVideoResult{URL: "http://127.0.0.1:1/video.mp4"},
	})
	if err != nil {
		t.Fatalf("convertPolledResponse(download fallback) error = %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Fatal("convertPolledResponse(download fallback) should include warning")
	}
	if resp.Videos[0].Type != "url" {
		t.Fatalf("convertPolledResponse fallback video type = %q", resp.Videos[0].Type)
	}
}

func TestFalVideoModelDoGenerateMissingRequestID(t *testing.T) {
	// Use a provider with an invalid host to force submit failure quickly.
	prov := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	model := NewVideoModel(prov, "luma-ray")
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "hello"})
	if err == nil {
		t.Fatal("DoGenerate should return provider error when submit fails")
	}
}

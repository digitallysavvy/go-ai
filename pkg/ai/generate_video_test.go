package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockVideoModel struct {
	maxPerCall *int
	generateFn func(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error)
}

func (m *mockVideoModel) SpecificationVersion() string { return "v3" }
func (m *mockVideoModel) Provider() string             { return "mock" }
func (m *mockVideoModel) ModelID() string              { return "mock-video" }
func (m *mockVideoModel) MaxVideosPerCall() *int       { return m.maxPerCall }
func (m *mockVideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	return m.generateFn(ctx, opts)
}

func TestGenerateVideoValidation(t *testing.T) {
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{})
	if err == nil || err.Error() != "model is required" {
		t.Fatalf("expected model required error, got %v", err)
	}

	model := &mockVideoModel{generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return &provider.VideoModelV3Response{}, nil
	}}
	_, err = GenerateVideo(context.Background(), GenerateVideoOptions{Model: model})
	if err == nil || err.Error() != "prompt text or image is required" {
		t.Fatalf("expected prompt required error, got %v", err)
	}
}

func TestGenerateVideoSingleCallDefaultsAndConversion(t *testing.T) {
	var gotOpts *provider.VideoModelV3CallOptions
	model := &mockVideoModel{
		generateFn: func(_ context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			gotOpts = opts
			return &provider.VideoModelV3Response{
				Videos: []provider.VideoModelV3VideoData{
					{Type: "url", URL: "https://example.com/video.mp4", MediaType: "video/mp4"},
				},
				Response: provider.VideoModelV3ResponseInfo{
					Timestamp: time.Unix(1710000000, 0).UTC(),
					ModelID:   "mock-video",
					Headers:   map[string]string{"x-request-id": "req-1"},
				},
				ProviderMetadata: map[string]interface{}{"requestId": "abc"},
			}, nil
		},
	}

	res, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "a sunny beach"},
	})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if gotOpts == nil || gotOpts.N != 1 {
		t.Fatalf("expected default N=1, got opts=%+v", gotOpts)
	}
	if res.Video == nil || res.Video.URL != "https://example.com/video.mp4" {
		t.Fatalf("unexpected primary video: %+v", res.Video)
	}
	if len(res.Videos) != 1 || len(res.Responses) != 1 {
		t.Fatalf("unexpected result sizes: videos=%d responses=%d", len(res.Videos), len(res.Responses))
	}
	if res.Responses[0].Headers["x-request-id"] != "req-1" {
		t.Fatalf("response headers not mapped: %+v", res.Responses[0].Headers)
	}
	if res.ProviderMetadata["requestId"] != "abc" {
		t.Fatalf("provider metadata not preserved: %+v", res.ProviderMetadata)
	}
}

func TestGenerateVideoParallelGenerate(t *testing.T) {
	maxPerCall := 2
	var callCount atomic.Int32
	model := &mockVideoModel{
		maxPerCall: &maxPerCall,
		generateFn: func(_ context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			n := callCount.Add(1)
			videos := make([]provider.VideoModelV3VideoData, 0, opts.N)
			for i := 0; i < opts.N; i++ {
				videos = append(videos, provider.VideoModelV3VideoData{
					Type:      "binary",
					Binary:    []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p'},
					MediaType: "video/mp4",
				})
			}
			return &provider.VideoModelV3Response{
				Videos: videos,
				Response: provider.VideoModelV3ResponseInfo{
					Timestamp: time.Unix(int64(1710000000+int64(n)), 0).UTC(),
					ModelID:   "mock-video",
				},
				ProviderMetadata: map[string]interface{}{"call": n},
			}, nil
		},
	}

	res, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "generate"},
		N:      3,
	})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if callCount.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount.Load())
	}
	if len(res.Videos) != 3 {
		t.Fatalf("expected 3 videos, got %d", len(res.Videos))
	}
	if len(res.Responses) != 2 {
		t.Fatalf("expected 2 response metadata entries, got %d", len(res.Responses))
	}
}

func TestGenerateVideoParallelError(t *testing.T) {
	maxPerCall := 1
	model := &mockVideoModel{
		maxPerCall: &maxPerCall,
		generateFn: func(_ context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			if opts.N == 1 {
				return nil, errors.New("provider failure")
			}
			return &provider.VideoModelV3Response{}, nil
		},
	}

	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "generate"},
		N:      2,
	})
	if err == nil || err.Error() != "provider failure" {
		t.Fatalf("expected provider failure, got %v", err)
	}
}

func TestConvertPromptImageAndVideoDataHelpers(t *testing.T) {
	if got := convertPromptImage(nil); got != nil {
		t.Fatalf("convertPromptImage(nil) = %+v, want nil", got)
	}

	urlImg := convertPromptImage(&VideoPromptImage{URL: "https://example.com/i.png"})
	if urlImg == nil || urlImg.Type != "url" || urlImg.URL == "" {
		t.Fatalf("unexpected url image conversion: %+v", urlImg)
	}

	fileImg := convertPromptImage(&VideoPromptImage{Data: []byte{0x89, 'P', 'N', 'G'}})
	if fileImg == nil || fileImg.Type != "file" || len(fileImg.Data) == 0 {
		t.Fatalf("unexpected file image conversion: %+v", fileImg)
	}

	urlFile, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type: "url", URL: "https://example.com/video.mp4", MediaType: "video/mp4",
	})
	if err != nil || urlFile.URL == "" {
		t.Fatalf("url conversion failed: file=%+v err=%v", urlFile, err)
	}

	base64File, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type: "base64", Data: "Zm9v", MediaType: "video/mp4",
	})
	if err != nil || string(base64File.Data) != "Zm9v" {
		t.Fatalf("base64 conversion failed: file=%+v err=%v", base64File, err)
	}

	_, err = convertVideoData(context.Background(), provider.VideoModelV3VideoData{Type: "weird"})
	if err == nil {
		t.Fatal("expected error for unknown video data type")
	}
}

func TestConvertToGenerateVideoResultNoVideos(t *testing.T) {
	model := &mockVideoModel{generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return &provider.VideoModelV3Response{}, nil
	}}
	_, err := convertToGenerateVideoResult(context.Background(), &provider.VideoModelV3Response{}, model)
	if err == nil {
		t.Fatal("expected no video generated error")
	}
}

func TestMergeProviderMetadata(t *testing.T) {
	merged := mergeProviderMetadata([]map[string]interface{}{
		{"a": 1, "b": "x"},
		{"b": "y", "c": true},
	})
	if merged["a"] != 1 || merged["b"] != "y" || merged["c"] != true {
		t.Fatalf("unexpected merged metadata: %+v", merged)
	}
	if mergeProviderMetadata(nil) != nil {
		t.Fatal("expected nil metadata for empty list")
	}
}

func TestConvertResponseInfoAndMin(t *testing.T) {
	info := convertResponseInfo(provider.VideoModelV3ResponseInfo{
		Timestamp: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		ModelID:   "m1",
		Headers:   map[string]string{"h": "v"},
	})
	if info.ModelID != "m1" || info.Timestamp == "" || info.Headers["h"] != "v" {
		t.Fatalf("unexpected response info: %+v", info)
	}
	if min(3, 7) != 3 || min(9, 4) != 4 {
		t.Fatalf("min() returned wrong values")
	}
}

func TestGenerateVideoNoVideosReturned(t *testing.T) {
	model := &mockVideoModel{
		generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			return &provider.VideoModelV3Response{
				Videos:   nil,
				Warnings: []types.Warning{{Type: "other", Message: "warn"}},
			}, nil
		},
	}
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "x"},
	})
	if err == nil {
		t.Fatal("expected no-video error")
	}
}

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockVideoModel struct {
	maxPerCall *int
	generateFn func(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error)
}

func (m *mockVideoModel) SpecificationVersion() string { return "v4" }
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
}

func TestGenerateVideoAllowsEmptyPromptLikeTypeScript(t *testing.T) {
	var gotPrompt string
	var gotPromptSet bool
	model := &mockVideoModel{generateFn: func(_ context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		gotPrompt = opts.Prompt
		gotPromptSet = opts.PromptSet
		return &provider.VideoModelV3Response{
			Videos: []provider.VideoModelV3VideoData{{
				Type:      "binary",
				Binary:    []byte{0, 0, 0, 0},
				MediaType: "video/mp4",
			}},
			Response: provider.VideoModelV3ResponseInfo{ModelID: "mock-video"},
		}, nil
	}}
	if _, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model}); err != nil {
		t.Fatalf("GenerateVideo empty prompt error = %v", err)
	}
	if gotPrompt != "" {
		t.Fatalf("prompt = %q, want empty string", gotPrompt)
	}
	if !gotPromptSet {
		t.Fatal("PromptSet = false, want true for explicit empty text prompt")
	}
}

func TestGenerateVideoFillsMissingResponseMetadataLikeRequiredTypeScriptResponse(t *testing.T) {
	model := &mockVideoModel{generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return &provider.VideoModelV3Response{
			Videos: []provider.VideoModelV3VideoData{{
				Type:      "binary",
				Binary:    []byte("video"),
				MediaType: "video/mp4",
			}},
		}, nil
	}}
	result, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if len(result.Responses) != 1 || result.Responses[0].ModelID != model.ModelID() || result.Responses[0].Timestamp.IsZero() {
		t.Fatalf("responses = %#v, want fallback model/timestamp metadata", result.Responses)
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
		Download: func(_ context.Context, url string) ([]byte, error) {
			if url != "https://example.com/video.mp4" {
				t.Fatalf("download url = %q", url)
			}
			return []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p'}, nil
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if gotOpts == nil || gotOpts.N != 1 {
		t.Fatalf("expected default N=1, got opts=%+v", gotOpts)
	}
	if gotOpts.ProviderOptions == nil {
		t.Fatal("expected providerOptions to default to an empty map")
	}
	if !strings.Contains(gotOpts.Headers["user-agent"], "go-ai/") {
		t.Fatalf("expected go-ai user-agent suffix, got headers=%+v", gotOpts.Headers)
	}
	if res.Video == nil || res.Video.URL != "" || !bytes.Equal(res.Video.Data, []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p'}) {
		t.Fatalf("unexpected primary video: %+v", res.Video)
	}
	if len(res.Videos) != 1 || len(res.Responses) != 1 {
		t.Fatalf("unexpected result sizes: videos=%d responses=%d", len(res.Videos), len(res.Responses))
	}
	if res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want empty slice", res.Warnings)
	}
	if res.Responses[0].Headers["x-request-id"] != "req-1" {
		t.Fatalf("response headers not mapped: %+v", res.Responses[0].Headers)
	}
	if !res.Responses[0].Timestamp.Equal(time.Unix(1710000000, 0).UTC()) {
		t.Fatalf("response timestamp = %s", res.Responses[0].Timestamp)
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, present := range []string{`"video"`, `"videos"`, `"warnings"`, `"responses"`, `"providerMetadata"`, `"modelId"`, `"timestamp"`} {
		if !strings.Contains(string(encoded), present) {
			t.Fatalf("result JSON = %s, missing %s", encoded, present)
		}
	}
	if strings.Contains(string(encoded), `"ModelID"`) || strings.Contains(string(encoded), `"ProviderMetadata"`) {
		t.Fatalf("result JSON = %s, must use TypeScript-compatible field names", encoded)
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

func TestGenerateVideoParallelPreservesCallOrderLikeTypeScript(t *testing.T) {
	maxPerCall := 2
	model := &mockVideoModel{
		maxPerCall: &maxPerCall,
		generateFn: func(_ context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			if opts.N == 2 {
				time.Sleep(20 * time.Millisecond)
				return &provider.VideoModelV3Response{
					Videos: []provider.VideoModelV3VideoData{
						{Type: "binary", Binary: []byte("a"), MediaType: "video/mp4"},
						{Type: "binary", Binary: []byte("b"), MediaType: "video/mp4"},
					},
					Response: provider.VideoModelV3ResponseInfo{
						Timestamp: time.Unix(1710000001, 0).UTC(),
						ModelID:   "mock-video",
					},
				}, nil
			}
			return &provider.VideoModelV3Response{
				Videos: []provider.VideoModelV3VideoData{{
					Type:      "binary",
					Binary:    []byte("c"),
					MediaType: "video/mp4",
				}},
				Response: provider.VideoModelV3ResponseInfo{
					Timestamp: time.Unix(1710000002, 0).UTC(),
					ModelID:   "mock-video",
				},
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
	got := string(res.Videos[0].Data) + string(res.Videos[1].Data) + string(res.Videos[2].Data)
	if got != "abc" {
		t.Fatalf("videos order = %q, want abc", got)
	}
}

func TestGenerateVideoDefaultRetriesTransientProviderError(t *testing.T) {
	attempts := 0
	model := &mockVideoModel{
		generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			attempts++
			if attempts == 1 {
				return nil, &providererrors.ProviderError{
					Provider:        "mock",
					StatusCode:      500,
					Message:         "temporary",
					ResponseHeaders: map[string]string{"retry-after-ms": "0"},
				}
			}
			return &provider.VideoModelV3Response{
				Videos: []provider.VideoModelV3VideoData{{
					Type:      "binary",
					Binary:    []byte("video"),
					MediaType: "video/mp4",
				}},
			}, nil
		},
	}

	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:  model,
		Prompt: VideoPrompt{Text: "generate"},
	})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestGenerateVideoMaxRetriesZeroDisablesRetries(t *testing.T) {
	attempts := 0
	model := &mockVideoModel{
		generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			attempts++
			return nil, &providererrors.ProviderError{
				Provider:        "mock",
				StatusCode:      500,
				Message:         "temporary",
				ResponseHeaders: map[string]string{"retry-after-ms": "0"},
			}
		},
	}
	maxRetries := 0
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:      model,
		Prompt:     VideoPrompt{Text: "generate"},
		MaxRetries: &maxRetries,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestGenerateVideoRejectsNegativeMaxRetries(t *testing.T) {
	model := &mockVideoModel{
		generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			t.Fatal("DoGenerate should not be called")
			return nil, nil
		},
	}
	maxRetries := -1
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:      model,
		Prompt:     VideoPrompt{Text: "generate"},
		MaxRetries: &maxRetries,
	})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Field != "maxRetries" {
		t.Fatalf("expected maxRetries invalid argument error, got %v", err)
	}
}

func TestGenerateVideoRejectsInvalidPromptImageDataStringBeforeProviderCall(t *testing.T) {
	model := &mockVideoModel{
		generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			t.Fatal("DoGenerate should not be called for invalid prompt image data")
			return nil, nil
		},
	}
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model: model,
		Prompt: VideoPrompt{Image: &VideoPromptImage{
			DataString: "not base64!!!",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid video prompt image data string") {
		t.Fatalf("expected invalid data string error, got %v", err)
	}
}

func TestGenerateVideoRejectsNonPositiveMaxVideosPerCall(t *testing.T) {
	zero := 0
	model := &mockVideoModel{
		generateFn: func(_ context.Context, _ *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			t.Fatal("DoGenerate should not be called")
			return nil, nil
		},
	}
	_, err := GenerateVideo(context.Background(), GenerateVideoOptions{
		Model:            model,
		Prompt:           VideoPrompt{Text: "generate"},
		MaxVideosPerCall: &zero,
	})
	if err == nil || err.Error() != "maxVideosPerCall must be > 0" {
		t.Fatalf("expected maxVideosPerCall error, got %v", err)
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
	if got, err := convertPromptImage(nil); err != nil || got != nil {
		t.Fatalf("convertPromptImage(nil) = %+v, want nil", got)
	}

	urlImg, err := convertPromptImage(&VideoPromptImage{URL: "https://example.com/i.png"})
	if err != nil {
		t.Fatalf("url image conversion error = %v", err)
	}
	if urlImg == nil || urlImg.Type != "url" || urlImg.URL == "" {
		t.Fatalf("unexpected url image conversion: %+v", urlImg)
	}

	fileImg, err := convertPromptImage(&VideoPromptImage{Data: []byte{0x89, 'P', 'N', 'G'}})
	if err != nil {
		t.Fatalf("file image conversion error = %v", err)
	}
	if fileImg == nil || fileImg.Type != "file" || len(fileImg.Data) == 0 {
		t.Fatalf("unexpected file image conversion: %+v", fileImg)
	}

	unknownFileImg, err := convertPromptImage(&VideoPromptImage{Data: []byte("not-an-image")})
	if err != nil {
		t.Fatalf("unknown file image conversion error = %v", err)
	}
	if unknownFileImg == nil || unknownFileImg.MediaType != "image/png" {
		t.Fatalf("unexpected raw image fallback media type: %+v", unknownFileImg)
	}

	unknownBase64Img, err := convertPromptImage(&VideoPromptImage{DataString: "bm90LWFuLWltYWdl"})
	if err != nil {
		t.Fatalf("unknown base64 image conversion error = %v", err)
	}
	if unknownBase64Img == nil || unknownBase64Img.MediaType != "image/png" {
		t.Fatalf("unexpected base64 image fallback media type: %+v", unknownBase64Img)
	}

	base64Img, err := convertPromptImage(&VideoPromptImage{DataString: "iVBORw0KGgo="})
	if err != nil {
		t.Fatalf("base64 image conversion error = %v", err)
	}
	if base64Img == nil || base64Img.Type != "file" || base64Img.MediaType != "image/png" || !bytes.Equal(base64Img.Data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("unexpected base64 image conversion: %+v", base64Img)
	}

	dataURLImg, err := convertPromptImage(&VideoPromptImage{DataString: "data:image/jpeg;base64,AQI="})
	if err != nil {
		t.Fatalf("data URL image conversion error = %v", err)
	}
	if dataURLImg == nil || dataURLImg.Type != "file" || dataURLImg.MediaType != "image/jpeg" || !bytes.Equal(dataURLImg.Data, []byte{1, 2}) {
		t.Fatalf("unexpected data URL image conversion: %+v", dataURLImg)
	}

	dataURLDefaultImg, err := convertPromptImage(&VideoPromptImage{DataString: "data:;base64,AQI="})
	if err != nil {
		t.Fatalf("default data URL image conversion error = %v", err)
	}
	if dataURLDefaultImg == nil || dataURLDefaultImg.MediaType != "image/png" {
		t.Fatalf("unexpected default data URL media type: %+v", dataURLDefaultImg)
	}

	urlFile, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type: "url", URL: "https://example.com/video.mp4", MediaType: "video/mp4",
	}, func(_ context.Context, url string) ([]byte, error) {
		if url != "https://example.com/video.mp4" {
			t.Fatalf("download url = %q", url)
		}
		return []byte{0x1A, 0x45, 0xDF, 0xA3}, nil
	}, nil)
	if err != nil || urlFile.URL != "" || !bytes.Equal(urlFile.Data, []byte{0x1A, 0x45, 0xDF, 0xA3}) || urlFile.MediaType != "video/mp4" {
		t.Fatalf("url conversion failed: file=%+v err=%v", urlFile, err)
	}

	base64File, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type: "base64", Data: "Zm9v", MediaType: "video/mp4",
	}, nil, nil)
	if err != nil || string(base64File.Data) != "foo" {
		t.Fatalf("base64 conversion failed: file=%+v err=%v", base64File, err)
	}

	_, err = convertVideoData(context.Background(), provider.VideoModelV3VideoData{Type: "weird"}, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown video data type")
	}
}

func TestConvertVideoDataDownloadsURLsAndDefaultsMediaTypeLikeTypeScript(t *testing.T) {
	file, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type:      "url",
		URL:       "https://example.com/video",
		MediaType: "application/octet-stream",
	}, func(_ context.Context, url string) ([]byte, error) {
		if url != "https://example.com/video" {
			t.Fatalf("download url = %q", url)
		}
		return []byte("not-enough-to-detect"), nil
	}, nil)
	if err != nil {
		t.Fatalf("convertVideoData() error = %v", err)
	}
	if file.URL != "" {
		t.Fatalf("expected URL output to be downloaded into bytes, got URL %q", file.URL)
	}
	if string(file.Data) != "not-enough-to-detect" {
		t.Fatalf("downloaded data not preserved: %q", string(file.Data))
	}
	if file.MediaType != "video/mp4" {
		t.Fatalf("media type = %q, want video/mp4 fallback", file.MediaType)
	}
}

func TestConvertVideoDataUsesCustomDownloadMediaTypeLikeTypeScript(t *testing.T) {
	file, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type: "url",
		URL:  "https://example.com/video",
	}, nil, func(_ context.Context, url string) (*DownloadResult, error) {
		if url != "https://example.com/video" {
			t.Fatalf("download url = %q", url)
		}
		return &DownloadResult{
			Data:      []byte("not-enough-to-detect"),
			MediaType: "video/webm",
		}, nil
	})
	if err != nil {
		t.Fatalf("convertVideoData() error = %v", err)
	}
	if file.MediaType != "video/webm" {
		t.Fatalf("media type = %q, want custom download media type video/webm", file.MediaType)
	}
	if string(file.Data) != "not-enough-to-detect" {
		t.Fatalf("downloaded data not preserved: %q", string(file.Data))
	}
}

func TestConvertVideoDataBase64DefaultsMediaTypeLikeTypeScript(t *testing.T) {
	file, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{
		Type: "base64",
		Data: "aGVsbG8",
	}, nil, nil)
	if err != nil {
		t.Fatalf("convertVideoData() error = %v", err)
	}
	if string(file.Data) != "hello" {
		t.Fatalf("decoded data = %q, want hello", string(file.Data))
	}
	if file.MediaType != "video/mp4" {
		t.Fatalf("media type = %q, want video/mp4", file.MediaType)
	}
	if file.Base64() != "aGVsbG8=" {
		t.Fatalf("base64 accessor = %q, want aGVsbG8=", file.Base64())
	}
	backing := file.Uint8Array()
	backing[0] = 'H'
	if string(file.Data) != "Hello" {
		t.Fatalf("Uint8Array should return backing data like TypeScript, data now %q", string(file.Data))
	}
}

func TestConvertToGenerateVideoResultNoVideos(t *testing.T) {
	model := &mockVideoModel{generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
		return &provider.VideoModelV3Response{}, nil
	}}
	response := &provider.VideoModelV3Response{
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Unix(1710000000, 0).UTC(),
			ModelID:   "mock-video",
			Headers:   map[string]string{"x": "y"},
		},
		ProviderMetadata: map[string]interface{}{"mock": map[string]interface{}{"requestId": "req-1"}},
	}
	_, err := convertToGenerateVideoResult(context.Background(), response, model, nil, nil)
	var noVideo *NoVideoGeneratedError
	if !errors.As(err, &noVideo) {
		t.Fatalf("expected no video generated error, got %v", err)
	}
	if len(noVideo.Responses) != 1 || noVideo.Responses[0].Headers["x"] != "y" {
		t.Fatalf("responses not preserved in error: %+v", noVideo.Responses)
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
	empty := mergeProviderMetadata(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty metadata map, got %#v", empty)
	}
}

func TestMergeProviderMetadataConcatenatesVideosLikeTypeScript(t *testing.T) {
	merged := mergeProviderMetadata([]map[string]interface{}{
		{"gateway": map[string]interface{}{
			"videos":  []interface{}{map[string]interface{}{"seed": 111}},
			"routing": map[string]interface{}{"provider": "fal"},
		}},
		{"gateway": map[string]interface{}{
			"videos": []map[string]interface{}{{"seed": 222}},
			"cost":   "0.08",
		}},
	})
	gateway := merged["gateway"].(map[string]interface{})
	videos := gateway["videos"].([]interface{})
	if len(videos) != 2 || gateway["cost"] != "0.08" {
		t.Fatalf("unexpected gateway metadata merge: %#v", gateway)
	}
}

func TestConvertResponseInfoAndMin(t *testing.T) {
	info := convertResponseInfo(provider.VideoModelV3ResponseInfo{
		Timestamp: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		ModelID:   "m1",
		Headers:   map[string]string{"h": "v"},
	}, map[string]interface{}{"mock": map[string]interface{}{"requestId": "req"}}, &mockVideoModel{})
	if info.ModelID != "m1" || !info.Timestamp.Equal(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)) || info.Headers["h"] != "v" || info.ProviderMetadata["mock"] == nil {
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

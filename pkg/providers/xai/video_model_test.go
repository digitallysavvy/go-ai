package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoModel_Metadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, "grok-imagine-video")

	assert.Equal(t, "v3", model.SpecificationVersion())
	assert.Equal(t, "xai", model.Provider())
	assert.Equal(t, "grok-imagine-video", model.ModelID())
	assert.NotNil(t, model.MaxVideosPerCall())
	assert.Equal(t, 1, *model.MaxVideosPerCall())
}

func TestVideoModel_TextToVideo(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			// First request: video generation
			assert.Equal(t, "/videos/generations", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)

			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "grok-imagine-video", body["model"])
			assert.Equal(t, "A cat playing piano", body["prompt"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			// Subsequent requests: status polling
			assert.Equal(t, "/videos/test-request-id", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url":      "https://example.com/video.mp4",
					"duration": 5.0,
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A cat playing piano",
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Videos, 1)
	assert.Equal(t, "url", resp.Videos[0].Type)
	assert.Equal(t, "https://example.com/video.mp4", resp.Videos[0].URL)
	assert.Equal(t, "video/mp4", resp.Videos[0].MediaType)

	// Check metadata
	assert.Contains(t, resp.ProviderMetadata, "xai")
	xaiMeta := resp.ProviderMetadata["xai"].(map[string]interface{})
	assert.Equal(t, "test-request-id", xaiMeta["requestId"])
	assert.Equal(t, "https://example.com/video.mp4", xaiMeta["videoUrl"])
	assert.Equal(t, 5.0, xaiMeta["duration"])
}

func TestVideoModel_ImageToVideo(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			assert.Equal(t, "/videos/generations", r.URL.Path)

			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Contains(t, body, "image")
			image := body["image"].(map[string]interface{})
			assert.Contains(t, image, "url")

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "Animate this scene",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/image.png",
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Videos, 1)
}

func TestVideoModel_ImageToVideo_Base64(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Contains(t, body, "image")
			image := body["image"].(map[string]interface{})
			assert.Contains(t, image, "url")
			// Check it's a data URL
			assert.Contains(t, image["url"], "data:image/png;base64,")

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "Animate this scene",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      []byte("fake-image-data"),
			MediaType: "image/png",
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestVideoModel_VideoEditing(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			// Should use edits endpoint
			assert.Equal(t, "/videos/edits", r.URL.Path)

			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Contains(t, body, "video")
			video := body["video"].(map[string]interface{})
			assert.Equal(t, "https://example.com/source.mp4", video["url"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/edited.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	videoURL := "https://example.com/source.mp4"
	opts := &provider.VideoModelV3CallOptions{
		Prompt: "Add dramatic lighting",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": 1,
				"videoUrl":       videoURL,
			},
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "https://example.com/edited.mp4", resp.Videos[0].URL)
}

func TestVideoModel_VideoExtensionMode(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			assert.Equal(t, "/videos/extensions", r.URL.Path)
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			video := body["video"].(map[string]interface{})
			assert.Equal(t, "https://example.com/source.mp4", video["url"])
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video":  map[string]interface{}{"url": "https://example.com/extended.mp4"},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	duration := 6.0
	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:   "Continue scene",
		Duration: &duration,
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": 1,
				"mode":           "extend-video",
				"videoUrl":       "https://example.com/source.mp4",
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/extended.mp4", resp.Videos[0].URL)
}

func TestVideoModel_ExtendMethod(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			assert.Equal(t, "/videos/extensions", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video":  map[string]interface{}{"url": "https://example.com/extended.mp4"},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")
	resp, err := model.Extend(context.Background(), VideoExtendOptions{
		Video:  "https://example.com/source.mp4",
		Prompt: "Continue",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/extended.mp4", resp.Videos[0].URL)
}

func TestVideoModel_ReferenceImagesMode(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			assert.Equal(t, "/videos/generations", r.URL.Path)
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			refs, ok := body["reference_images"].([]interface{})
			require.True(t, ok)
			require.Len(t, refs, 2)
			assert.Equal(t, "https://example.com/ref1.png", refs[0].(map[string]interface{})["url"])
			assert.Equal(t, "https://example.com/ref2.png", refs[1].(map[string]interface{})["url"])
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video":  map[string]interface{}{"url": "https://example.com/ref-video.mp4"},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "Use refs",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs":     1,
				"mode":               "reference-to-video",
				"referenceImageUrls": []string{"https://example.com/ref1.png", "https://example.com/ref2.png"},
			},
		},
	})
	require.NoError(t, err)
}

func TestVideoModel_ReferenceImagesValidation(t *testing.T) {
	prov := New(Config{APIKey: "test-key", BaseURL: "https://example.com"})
	model := NewVideoModel(prov, "grok-imagine-video")

	cases := []struct {
		name string
		urls []string
	}{
		{name: "empty", urls: []string{}},
		{name: "too many", urls: []string{
			"https://example.com/ref-1.jpg",
			"https://example.com/ref-2.jpg",
			"https://example.com/ref-3.jpg",
			"https://example.com/ref-4.jpg",
			"https://example.com/ref-5.jpg",
			"https://example.com/ref-6.jpg",
			"https://example.com/ref-7.jpg",
			"https://example.com/ref-8.jpg",
		}},
		{name: "empty url", urls: []string{"https://example.com/ref-1.jpg", ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
				Prompt: "Use refs",
				ProviderOptions: map[string]interface{}{
					"xai": map[string]interface{}{"referenceImageUrls": tc.urls},
				},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "referenceImageUrls")
		})
	}
}

func TestVideoModel_ExtendWarningsSurfaced(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video":  map[string]interface{}{"url": "https://example.com/extended.mp4"},
			"warnings": []map[string]interface{}{
				{"code": "DURATION_CAPPED", "message": "Duration capped to 10 seconds"},
			},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")
	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "Continue",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"mode": "extend-video", "videoUrl": "https://example.com/source.mp4", "pollIntervalMs": 1},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Warnings)
	assert.Equal(t, "provider-warning", resp.Warnings[0].Type)
	assert.Equal(t, "Duration capped to 10 seconds", resp.Warnings[0].Details)
}

func TestVideoModel_WithDuration(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, 10.0, body["duration"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	duration := 10.0
	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
		Duration:        &duration,
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestVideoModel_WithAspectRatio(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "16:9", body["aspect_ratio"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
		AspectRatio:     "16:9",
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestVideoModel_UnknownResolutionWarning(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.NotContains(t, body, "resolution")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")
	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
		Resolution:      "3840x2160",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Warnings)
	assert.Equal(t, "unsupported", resp.Warnings[0].Type)
	assert.Equal(t, "resolution", resp.Warnings[0].Feature)
}

func TestVideoModel_WithResolution(t *testing.T) {
	tests := []struct {
		name           string
		resolution     string
		expectedInBody string
	}{
		{
			name:           "1280x720 maps to 720p",
			resolution:     "1280x720",
			expectedInBody: "720p",
		},
		{
			name:           "854x480 maps to 480p",
			resolution:     "854x480",
			expectedInBody: "480p",
		},
		{
			name:           "640x480 maps to 480p",
			resolution:     "640x480",
			expectedInBody: "480p",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++

				if requestCount == 1 {
					var body map[string]interface{}
					err := json.NewDecoder(r.Body).Decode(&body)
					require.NoError(t, err)

					assert.Equal(t, tt.expectedInBody, body["resolution"])

					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"request_id": "test-request-id",
					})
				} else {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"status": "done",
						"video": map[string]interface{}{
							"url": "https://example.com/video.mp4",
						},
					})
				}
			}))
			defer server.Close()

			prov := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			model := NewVideoModel(prov, "grok-imagine-video")

			opts := &provider.VideoModelV3CallOptions{
				ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
				Prompt:          "A sunset",
				Resolution:      tt.resolution,
			}

			ctx := context.Background()
			resp, err := model.DoGenerate(ctx, opts)

			require.NoError(t, err)
			require.NotNil(t, resp)
		})
	}
}

func TestVideoModel_WithProviderResolution(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "720p", body["resolution"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": 1,
				"resolution":     "720p",
			},
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestVideoModel_UnsupportedOptions_Warnings(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	fps := 30
	seed := 12345
	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
		FPS:             &fps,
		Seed:            &seed,
		N:               3,
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should have warnings for fps, seed, and n > 1
	assert.Len(t, resp.Warnings, 3)

	warningMessages := make([]string, len(resp.Warnings))
	warningFeatures := make([]string, len(resp.Warnings))
	for i, w := range resp.Warnings {
		assert.Equal(t, "unsupported", w.Type)
		warningMessages[i] = w.Details
		warningFeatures[i] = w.Feature
	}

	assert.Contains(t, warningFeatures, "fps")
	assert.Contains(t, warningFeatures, "seed")
	assert.Contains(t, warningFeatures, "n")
	assert.Contains(t, warningMessages, "xAI video models do not support custom FPS.")
	assert.Contains(t, warningMessages, "xAI video models do not support seed.")
	assert.Contains(t, warningMessages, "xAI video models do not support generating multiple videos per call. Only 1 video will be generated.")
}

func TestVideoModel_VideoEditing_UnsupportedOptions_Warnings(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	duration := 5.0
	videoURL := "https://example.com/source.mp4"
	opts := &provider.VideoModelV3CallOptions{
		Prompt:      "Add effects",
		Duration:    &duration,
		AspectRatio: "16:9",
		Resolution:  "1280x720",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": 1,
				"videoUrl":       videoURL,
			},
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should have warnings for duration, aspect ratio, and resolution on edits
	assert.GreaterOrEqual(t, len(resp.Warnings), 3)
}

func TestVideoModel_CustomPollInterval(t *testing.T) {
	requestCount := 0
	pollStart := time.Now()
	var pollDuration time.Duration

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		switch requestCount {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		case 2:
			// First poll - return pending
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "pending",
			})
		default:
			// Second poll - return done
			pollDuration = time.Since(pollStart)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	pollIntervalMs := 100
	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": pollIntervalMs,
			},
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Poll duration should be at least the interval
	assert.GreaterOrEqual(t, pollDuration.Milliseconds(), int64(pollIntervalMs))
}

func TestVideoModel_StatusExpired(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "expired",
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
	}

	ctx := context.Background()
	_, err := model.DoGenerate(ctx, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestVideoModel_StatusFailed(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "failed"})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}}, Prompt: "A sunset"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestVideoModel_ProviderMetadataProgress(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "done",
			"progress": 100,
			"video":    map[string]interface{}{"url": "https://example.com/video.mp4"},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}}, Prompt: "A sunset"})
	require.NoError(t, err)
	xaiMeta := resp.ProviderMetadata["xai"].(map[string]interface{})
	assert.Equal(t, 100, xaiMeta["progress"])
}

func TestVideoModel_NoRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			// Missing request_id
		})
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
	}

	ctx := context.Background()
	_, err := model.DoGenerate(ctx, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "No request_id")
}

func TestVideoModel_NoVideoURL(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				// Missing video or video.url
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A sunset",
	}

	ctx := context.Background()
	_, err := model.DoGenerate(ctx, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no video URL")
}

// TestXAIVideoModerationError verifies that a moderated video status is
// surfaced as a normal job-failed error (via polling.JobResult), not a
// thrown ModerationError/ProviderError.
func TestXAIVideoModerationError(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url":                "https://example.com/video.mp4",
					"respect_moderation": false,
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 1}},
		Prompt:          "A violent scene",
	}

	ctx := context.Background()
	_, err := model.DoGenerate(ctx, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "content policy violation")
}

// TestXAIVideoPassthroughOptions verifies that unrecognized provider options are passed
// through to the video generation request body.
func TestXAIVideoPassthroughOptions(t *testing.T) {
	requestCount := 0
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	guidance := float64(7)
	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cinematic sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": 1,
				"style":          "cinematic",
				"guidance_scale": guidance,
			},
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)

	if capturedBody == nil {
		t.Skip("server not reached")
	}
	assert.Equal(t, "cinematic", capturedBody["style"])
	assert.Equal(t, guidance, capturedBody["guidance_scale"])
}

// TestXAIVideoCostMetadata verifies that costInUsdTicks from the video status response
// is included in the ProviderMetadata of the result.
func TestXAIVideoCostMetadata(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request_id": "test-request-id",
			})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video": map[string]interface{}{
					"url": "https://example.com/video.mp4",
				},
				"usage": map[string]interface{}{
					"cost_in_usd_ticks": int64(150),
				},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset timelapse",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"pollIntervalMs": 10,
				"pollTimeoutMs":  500,
			},
		},
	}

	ctx := context.Background()
	resp, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Check that costInUsdTicks is in provider metadata.
	assert.Contains(t, resp.ProviderMetadata, "xai")
	xaiMeta, ok := resp.ProviderMetadata["xai"].(map[string]interface{})
	require.True(t, ok, "xai metadata type = %T, want map[string]interface{}", resp.ProviderMetadata["xai"])
	assert.Contains(t, xaiMeta, "costInUsdTicks")
}

// TestVideoModel_Resolution1080p_MapsFromSize verifies that the standard
// "1920x1080" size maps to the xAI "1080p" resolution value.
func TestVideoModel_Resolution1080p_MapsFromSize(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "1080p", body["resolution"])
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt:     "A sunset",
		Resolution: "1920x1080",
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestVideoModel_Resolution1080p_OldModelWarns verifies that requesting
// 1080p on the original grok-imagine-video model still sends the request but
// adds an unsupported warning.
func TestVideoModel_Resolution1080p_OldModelWarns(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "1080p", body["resolution"])
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"resolution": "1080p"},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)

	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w.Message, "does not support 1080p") {
			found = true
		}
	}
	assert.True(t, found, "expected an unsupported-1080p warning, got: %+v", resp.Warnings)
}

// TestVideoModel_ExtendVideo verifies that mode "extend-video" hits the
// /v1/videos/extensions endpoint, sends the source video, allows duration,
// but does not send `user`.
func TestVideoModel_ExtendVideo(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			assert.Equal(t, "/videos/extensions", r.URL.Path)

			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			video, ok := body["video"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "https://example.com/source.mp4", video["url"])
			assert.Equal(t, 5.0, body["duration"])
			assert.NotContains(t, body, "user")
			assert.NotContains(t, body, "aspect_ratio")
			assert.NotContains(t, body, "resolution")

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/extended.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	duration := 5.0
	user := "user-123"
	opts := &provider.VideoModelV3CallOptions{
		Prompt:      "Continue the scene",
		Duration:    &duration,
		AspectRatio: "16:9",
		Resolution:  "1280x720",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"mode":     "extend-video",
				"videoUrl": "https://example.com/source.mp4",
				"user":     user,
			},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "https://example.com/extended.mp4", resp.Videos[0].URL)

	// aspectRatio + resolution should have produced warnings for extension mode.
	assert.GreaterOrEqual(t, len(resp.Warnings), 2)
}

// TestVideoModel_ReferenceToVideo verifies R2V mode sends reference_images
// and reference_audios, and is capped at 720p.
func TestVideoModel_ReferenceToVideo(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			assert.Equal(t, "/videos/generations", r.URL.Path)

			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

			refImages, ok := body["reference_images"].([]interface{})
			require.True(t, ok, "expected reference_images in body, got: %+v", body)
			require.Len(t, refImages, 2)

			refAudios, ok := body["reference_audios"].([]interface{})
			require.True(t, ok, "expected reference_audios in body, got: %+v", body)
			require.Len(t, refAudios, 2)
			firstAudio := refAudios[0].(map[string]interface{})
			assert.Equal(t, "voice-1", firstAudio["voice_id"])

			// R2V requested 1080p; must be downgraded to 720p.
			assert.Equal(t, "720p", body["resolution"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/r2v.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A dancing robot",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"referenceImageUrls": []string{
					"https://example.com/ref1.png",
					"https://example.com/ref2.png",
				},
				"referenceVoiceIds": []string{"voice-1", "voice-2"},
				"resolution":        "1080p",
			},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "https://example.com/r2v.mp4", resp.Videos[0].URL)

	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w.Message, "downgraded from 1080p to 720p") {
			found = true
		}
	}
	assert.True(t, found, "expected a 1080p downgrade warning, got: %+v", resp.Warnings)
}

// TestVideoModel_ReferenceVoiceIds_TooMany verifies that more than 3
// referenceVoiceIds is a hard validation error, not a warning with silent
// truncation: TS's schema is z.array(nonEmptyStringSchema).max(3), and its
// test ("should reject more than 3 preset reference voices") asserts
// doStart rejects with InvalidArgumentError rather than sending a truncated
// request.
func TestVideoModel_ReferenceVoiceIds_TooMany(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request should be sent when referenceVoiceIds exceeds 3")
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A dancing robot",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"referenceImageUrls": []string{"https://example.com/ref1.png"},
				"referenceVoiceIds":  []string{"voice-1", "voice-2", "voice-3", "voice-4"},
			},
		},
	}

	_, err := model.DoGenerate(context.Background(), opts)
	require.Error(t, err)
	var invalidArg *providererrors.InvalidArgumentError
	require.ErrorAs(t, err, &invalidArg)
	assert.Equal(t, "referenceVoiceIds", invalidArg.Field)
}

// TestVideoModel_ReferenceVoiceIds_OutsideR2V_Warns verifies referenceVoiceIds
// are ignored (with a warning) when not in reference-to-video mode.
func TestVideoModel_ReferenceVoiceIds_OutsideR2V_Warns(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.NotContains(t, body, "reference_audios")

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"referenceVoiceIds": []string{"voice-1"},
			},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)

	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w.Message, "only supports reference voices for reference-to-video") {
			found = true
		}
	}
	assert.True(t, found, "expected an outside-R2V warning, got: %+v", resp.Warnings)
}

// TestVideoModel_User_SentForGeneration_NotForExtension verifies the `user`
// option is sent for standard generation but omitted for extend-video.
func TestVideoModel_User_SentForGeneration_NotForExtension(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "user-abc", body["user"])

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"user": "user-abc"},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestVideoModel_StatusPending202EmptyBody verifies that a 202 status
// response with an empty body is treated as "pending" rather than an error.
func TestVideoModel_StatusPending202EmptyBody(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch requestCount {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		case 2:
			// 202 with a completely empty body.
			w.WriteHeader(http.StatusAccepted)
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 50},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "https://example.com/video.mp4", resp.Videos[0].URL)
}

// TestVideoModel_StatusPending202InvalidJSON verifies that a 202 status
// response with an unparsable body is treated as "pending" rather than an
// error.
func TestVideoModel_StatusPending202InvalidJSON(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch requestCount {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		case 2:
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("not json"))
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "done",
				"video":  map[string]interface{}{"url": "https://example.com/video.mp4"},
			})
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 50},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "https://example.com/video.mp4", resp.Videos[0].URL)
}

// TestVideoModel_StatusPending202OversizedBodyErrors covers P1-5c item 8: a
// 202 status body larger than the 1 MiB bound is a real error, mirroring TS
// readPendingBody's APICallError, rather than being silently treated as
// pending forever.
func TestVideoModel_StatusPending202OversizedBodyErrors(t *testing.T) {
	oversized := strings.Repeat("a", 1024*1024+1)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch requestCount {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
		default:
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(oversized))
		}
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, "grok-imagine-video")

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 50},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "exceeded")
}

// doneVideoHandler returns an http.HandlerFunc that answers request 1 with
// a create response and every subsequent request with a "done" status
// response, capturing the create request body into gotBody.
func doneVideoHandler(t *testing.T, gotBody *map[string]interface{}, videoURL string) http.HandlerFunc {
	t.Helper()
	requestCount := 0
	return func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			require.NoError(t, json.NewDecoder(r.Body).Decode(gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video":  map[string]interface{}{"url": videoURL},
		})
	}
}

// TestVideoModel_MapsKeyframesLastFrameGenerateAudioStorageOptions ports TS
// "should map current xAI generation options"
// (xai-video-model.test.ts:149).
func TestVideoModel_MapsKeyframesLastFrameGenerateAudioStorageOptions(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://vidgen.x.ai/output/video-001.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	generateAudio := false
	opts := &provider.VideoModelV3CallOptions{
		Prompt:        "A chicken flying into the sunset",
		GenerateAudio: &generateAudio,
		FrameImages: []provider.VideoFrameImage{
			{
				FrameType: provider.VideoFrameTypeLastFrame,
				Image:     provider.VideoModelV3File{Type: "url", URL: "https://example.com/end.png"},
			},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"storageOptions": map[string]interface{}{
					"filename":     "result.mp4",
					"expiresAfter": 86_400,
					"publicUrl":    map[string]interface{}{"expiresAfter": 3_600},
				},
				"keyframes": []map[string]interface{}{
					{"imageUrl": "https://example.com/middle.png", "timestampSeconds": 2.5},
				},
				"pollIntervalMs": 1,
			},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, false, gotBody["generate_audio"])
	lastFrame, ok := gotBody["last_frame"].(map[string]interface{})
	require.True(t, ok, "expected last_frame in body, got: %+v", gotBody)
	assert.Equal(t, "https://example.com/end.png", lastFrame["url"])

	storageOptions, ok := gotBody["storage_options"].(map[string]interface{})
	require.True(t, ok, "expected storage_options in body, got: %+v", gotBody)
	assert.Equal(t, "result.mp4", storageOptions["filename"])
	assert.Equal(t, float64(86_400), storageOptions["expires_after"])
	publicURL, ok := storageOptions["public_url"].(map[string]interface{})
	require.True(t, ok, "expected public_url object, got: %+v", storageOptions["public_url"])
	assert.Equal(t, float64(3_600), publicURL["expires_after"])

	keyframes, ok := gotBody["keyframes"].([]interface{})
	require.True(t, ok, "expected keyframes in body, got: %+v", gotBody)
	require.Len(t, keyframes, 1)
	kf := keyframes[0].(map[string]interface{})
	kfImage := kf["image"].(map[string]interface{})
	assert.Equal(t, "https://example.com/middle.png", kfImage["url"])
	assert.Equal(t, 2.5, kf["timestamp_s"])
}

// TestVideoModel_LastFrame_WarnsForOldModel ports TS "should warn and omit
// last_frame for grok-imagine-video" (xai-video-model.test.ts:196).
func TestVideoModel_LastFrame_WarnsForOldModel(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		FrameImages: []provider.VideoFrameImage{
			{
				FrameType: provider.VideoFrameTypeLastFrame,
				Image:     provider.VideoModelV3File{Type: "url", URL: "https://example.com/end.png"},
			},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	_, hasLastFrame := gotBody["last_frame"]
	assert.False(t, hasLastFrame, "last_frame should be omitted, got body: %+v", gotBody)

	found := false
	for _, w := range resp.Warnings {
		if w.Feature == "frameImages" && strings.Contains(w.Message, `only supports last_frame with "grok-imagine-video-1.5"`) {
			found = true
		}
	}
	assert.True(t, found, "expected a last_frame unsupported warning, got: %+v", resp.Warnings)
}

// TestVideoModel_InputReferences_SeparatesImageAndAudio ports TS "should
// separate image and audio inputReferences" (xai-video-model.test.ts:962).
func TestVideoModel_InputReferences_SeparatesImageAndAudio(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/ref1.jpg"},
			{Type: "url", URL: "https://example.com/voice.mp3", MediaType: "audio/mpeg"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.Empty(t, resp.Warnings)

	refImages, ok := gotBody["reference_images"].([]interface{})
	require.True(t, ok)
	require.Len(t, refImages, 1)
	assert.Equal(t, "https://example.com/ref1.jpg", refImages[0].(map[string]interface{})["url"])

	refAudios, ok := gotBody["reference_audios"].([]interface{})
	require.True(t, ok)
	require.Len(t, refAudios, 1)
	assert.Equal(t, "https://example.com/voice.mp3", refAudios[0].(map[string]interface{})["url"])
}

// TestVideoModel_InputReferences_AudioOnlyR2V ports TS "should support
// audio-only reference-to-video" (xai-video-model.test.ts:986).
func TestVideoModel_InputReferences_AudioOnlyR2V(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/voice.mp3", MediaType: "audio/mpeg"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.Empty(t, resp.Warnings)

	_, hasRefImages := gotBody["reference_images"]
	assert.False(t, hasRefImages, "reference_images should be omitted, got: %+v", gotBody)
	refAudios, ok := gotBody["reference_audios"].([]interface{})
	require.True(t, ok)
	require.Len(t, refAudios, 1)
	assert.Equal(t, "https://example.com/voice.mp3", refAudios[0].(map[string]interface{})["url"])
}

// TestVideoModel_InputReferences_VideoOnly_NoEmptyReferenceImages ports TS
// "should not send an empty reference_images array for video-only
// inputReferences" (xai-video-model.test.ts:1008).
func TestVideoModel_InputReferences_VideoOnly_NoEmptyReferenceImages(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/clip.mp4", MediaType: "video/mp4"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	}

	_, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	_, hasRefImages := gotBody["reference_images"]
	assert.False(t, hasRefImages, "reference_images should be omitted, got: %+v", gotBody)
}

// TestVideoModel_InputReferences_FirstFrameWithAudioReference ports TS
// "should combine a pinned first frame with an audio reference"
// (xai-video-model.test.ts:1026).
func TestVideoModel_InputReferences_FirstFrameWithAudioReference(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/start.jpg", MediaType: "image/jpeg"},
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/voice.mp3", MediaType: "audio/mpeg"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.Empty(t, resp.Warnings)

	image, ok := gotBody["image"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://example.com/start.jpg", image["url"])
	_, hasRefImages := gotBody["reference_images"]
	assert.False(t, hasRefImages, "reference_images should be omitted, got: %+v", gotBody)
	refAudios, ok := gotBody["reference_audios"].([]interface{})
	require.True(t, ok)
	require.Len(t, refAudios, 1)
}

// TestVideoModel_InputReferences_ExplicitAudioOnlyR2V ports TS "should
// support explicit audio-only R2V without reference_images"
// (xai-video-model.test.ts:1054).
func TestVideoModel_InputReferences_ExplicitAudioOnlyR2V(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/voice.mp3", MediaType: "audio/mpeg"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"mode": "reference-to-video", "pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	require.Empty(t, resp.Warnings)

	_, hasRefImages := gotBody["reference_images"]
	assert.False(t, hasRefImages, "reference_images should be omitted, got: %+v", gotBody)
	refAudios, ok := gotBody["reference_audios"].([]interface{})
	require.True(t, ok)
	require.Len(t, refAudios, 1)
}

// TestVideoModel_ExplicitR2V_NoReferences_Warns ports TS "should warn when
// explicit R2V has no references at all" (xai-video-model.test.ts:1081).
func TestVideoModel_ExplicitR2V_NoReferences_Warns(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"mode": "reference-to-video", "pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	_, hasRefImages := gotBody["reference_images"]
	assert.False(t, hasRefImages, "reference_images should be omitted, got: %+v", gotBody)

	found := false
	for _, w := range resp.Warnings {
		if w.Feature == "referenceImages" && strings.Contains(w.Message, "without reference images") {
			found = true
		}
	}
	assert.True(t, found, "expected a no-references warning, got: %+v", resp.Warnings)
}

// TestVideoModel_InputReferences_CombinedAudioCapTruncatesWithWarning
// mirrors the xai-video-model.ts referenceAudioInputs truncation ("xAI
// reference-to-video supports at most 3 audio references. Only the first 3
// were used."): 2 audio inputReferences plus 2 referenceVoiceIds (4 total)
// must be capped at 3, with a warning rather than a hard error (only the
// standalone referenceVoiceIds array itself has a schema-level max of 3).
func TestVideoModel_InputReferences_CombinedAudioCapTruncatesWithWarning(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/ref.jpg"},
			{Type: "url", URL: "https://example.com/a1.mp3", MediaType: "audio/mpeg"},
			{Type: "url", URL: "https://example.com/a2.mp3", MediaType: "audio/mpeg"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"mode":              "reference-to-video",
				"referenceVoiceIds": []string{"voice-1", "voice-2"},
				"pollIntervalMs":    1,
			},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)

	refAudios, ok := gotBody["reference_audios"].([]interface{})
	require.True(t, ok)
	require.Len(t, refAudios, 3, "expected the combined list to be capped at 3, got: %+v", refAudios)

	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w.Message, "at most 3 audio references") {
			found = true
		}
	}
	assert.True(t, found, "expected a truncation warning, got: %+v", resp.Warnings)
}

// TestVideoModel_InputReferences_IgnoredOutsideR2V_Warns mirrors the
// xai-video-model.ts inputReferences-ignored warning: an explicit
// non-R2V mode (edit-video here) with inputReferences present must warn
// that the references were ignored.
func TestVideoModel_InputReferences_IgnoredOutsideR2V_Warns(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		InputReferences: []provider.VideoModelV3File{
			{Type: "url", URL: "https://example.com/ref.jpg"},
		},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"mode":           "edit-video",
				"videoUrl":       "https://example.com/source.mp4",
				"pollIntervalMs": 1,
			},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)

	found := false
	for _, w := range resp.Warnings {
		if w.Feature == "inputReferences" && strings.Contains(w.Message, "only supports inputReferences for reference-to-video") {
			found = true
		}
	}
	assert.True(t, found, "expected an inputReferences-ignored warning, got: %+v", resp.Warnings)
}

// TestVideoModel_StartImage_RejectsVideoFile mirrors xai-video-model.ts
// resolveStartImage / isVideoFile: a video file passed as the start image
// (or a pinned first_frame) is rejected with a warning rather than sent as
// `image`.
func TestVideoModel_StartImage_RejectsVideoFile(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(doneVideoHandler(t, &gotBody, "https://example.com/out.mp4"))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A chicken flying into the sunset",
		Image:  &provider.VideoModelV3File{Type: "url", URL: "https://example.com/clip.mp4", MediaType: "video/mp4"},
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	}

	resp, err := model.DoGenerate(context.Background(), opts)
	require.NoError(t, err)
	_, hasImage := gotBody["image"]
	assert.False(t, hasImage, "image should be omitted for a video file, got: %+v", gotBody)

	found := false
	for _, w := range resp.Warnings {
		if w.Feature == "image" && strings.Contains(w.Message, "does not accept a video as a start/frame image") {
			found = true
		}
	}
	assert.True(t, found, "expected a video-start-image warning, got: %+v", resp.Warnings)
}

// TestVideoModel_FileOutput_MapsToProviderMetadata mirrors the xAI status
// response's file_output/storage_error fields (persisted-video Files API
// metadata), and the URL fallback from file_output.public_url when the
// direct video.url is empty.
func TestVideoModel_FileOutput_MapsToProviderMetadata(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"request_id": "test-request-id"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done",
			"video": map[string]interface{}{
				"file_output": map[string]interface{}{
					"file_id":    "file-123",
					"filename":   "result.mp4",
					"public_url": "https://files.x.ai/result.mp4",
				},
			},
		})
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, ModelGrokImagineVideo15)

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"pollIntervalMs": 1},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "https://files.x.ai/result.mp4", resp.Videos[0].URL)

	xaiMeta := resp.ProviderMetadata["xai"].(map[string]interface{})
	fileOutput, ok := xaiMeta["fileOutput"].(map[string]interface{})
	require.True(t, ok, "expected fileOutput metadata, got: %+v", xaiMeta)
	assert.Equal(t, "file-123", fileOutput["fileId"])
	assert.Equal(t, "result.mp4", fileOutput["filename"])
	assert.Equal(t, "https://files.x.ai/result.mp4", fileOutput["publicUrl"])
}

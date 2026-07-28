package xai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
		Prompt: "A cat playing piano",
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
		Prompt: "Animate this scene",
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
		Prompt: "Animate this scene",
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
				"videoUrl": videoURL,
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
				"mode":     "extend-video",
				"videoUrl": "https://example.com/source.mp4",
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
			"xai": map[string]interface{}{"mode": "extend-video", "videoUrl": "https://example.com/source.mp4"},
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
		Prompt:   "A sunset",
		Duration: &duration,
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
		Prompt:      "A sunset",
		AspectRatio: "16:9",
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
		Prompt:     "A sunset",
		Resolution: "3840x2160",
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
				Prompt:     "A sunset",
				Resolution: tt.resolution,
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
				"resolution": "720p",
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
		Prompt: "A sunset",
		FPS:    &fps,
		Seed:   &seed,
		N:      3,
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
				"videoUrl": videoURL,
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
		Prompt: "A sunset",
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

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "A sunset"})
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

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "A sunset"})
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
		Prompt: "A sunset",
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
		Prompt: "A sunset",
	}

	ctx := context.Background()
	_, err := model.DoGenerate(ctx, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no video URL")
}

// TestXAIVideoModerationError verifies that a moderated video status returns a ModerationError.
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
		Prompt: "A violent scene",
	}

	ctx := context.Background()
	_, err := model.DoGenerate(ctx, opts)

	require.Error(t, err)

	// Must be (or wrap) a ModerationError.
	var modErr *ModerationError
	require.True(t, errors.As(err, &modErr), "error type = %T, want *ModerationError (possibly wrapped)", err)
	assert.Contains(t, modErr.Message, "content policy violation")
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

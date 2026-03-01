package bytedance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// testServer holds a mock HTTP server and request tracking
type testServer struct {
	server       *httptest.Server
	requests     []*recordedRequest
	createBody   map[string]interface{}
	createStatus string
	statusBody   string
}

type recordedRequest struct {
	method string
	path   string
	body   map[string]interface{}
}

// newTestServer creates a mock server that simulates the ByteDance API.
// It handles two endpoints:
//   - POST /contents/generations/tasks       → returns createTaskID
//   - GET  /contents/generations/tasks/<id>  → cycles through statusResponses
func newTestServer(t *testing.T, createTaskID string, statusResponses []string) *testServer {
	ts := &testServer{}

	mux := http.NewServeMux()

	callCount := 0

	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			// Record request body
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Logf("failed to decode request body: %v", err)
			}
			ts.requests = append(ts.requests, &recordedRequest{
				method: r.Method,
				path:   r.URL.Path,
				body:   body,
			})
			ts.createBody = body

			w.Header().Set("Content-Type", "application/json")
			if createTaskID == "" {
				// Simulate empty id response
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{}`)
			} else {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{"id": %q}`, createTaskID)
			}

		default:
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/api/v3/contents/generations/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		ts.requests = append(ts.requests, &recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
		})

		idx := callCount
		if idx >= len(statusResponses) {
			idx = len(statusResponses) - 1
		}
		callCount++

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, statusResponses[idx])
	})

	ts.server = httptest.NewServer(mux)
	return ts
}

// newErrorServer creates a mock server that returns an API error on task creation
func newErrorServer(t *testing.T, statusCode int, errorBody string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprintln(w, errorBody)
	})
	return httptest.NewServer(mux)
}

// providerForServer creates a ByteDance provider pointing to the given test server URL
func providerForServer(t *testing.T, serverURL string) *Provider {
	t.Helper()

	// Strip trailing slash from server URL and use it as base
	baseURL := strings.TrimSuffix(serverURL, "/") + "/api/v3"

	prov, err := New(Config{
		APIKey:  "test-key",
		BaseURL: baseURL,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return prov
}

var defaultOpts = &provider.VideoModelV3CallOptions{
	Prompt: "A futuristic city with flying cars",
	N:      1,
}

func makeSuccessStatusBody(taskID, videoURL string) string {
	return fmt.Sprintf(`{
		"id": %q,
		"model": "seedance-1-0-pro-250528",
		"status": "succeeded",
		"content": {"video_url": %q},
		"usage": {"completion_tokens": 100}
	}`, taskID, videoURL)
}

// ─── Request Body Builder Tests ───────────────────────────────────────────────

func TestBuildRequestBody_Prompt(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A cherry blossom tree in the wind",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := ts.createBody
	if body["model"] != "seedance-1-0-pro-250528" {
		t.Errorf("expected model 'seedance-1-0-pro-250528', got %v", body["model"])
	}

	content, ok := body["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected content array with 1 item, got %v", body["content"])
	}

	item := content[0].(map[string]interface{})
	if item["type"] != "text" {
		t.Errorf("expected content type 'text', got %v", item["type"])
	}
	if item["text"] != "A cherry blossom tree in the wind" {
		t.Errorf("expected text prompt, got %v", item["text"])
	}
}

func TestBuildRequestBody_AspectRatio(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:      "test prompt",
		AspectRatio: "16:9",
		N:           1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["ratio"] != "16:9" {
		t.Errorf("expected ratio '16:9', got %v", ts.createBody["ratio"])
	}
}

func TestBuildRequestBody_Duration(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	dur := 5.0
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:   "test prompt",
		Duration: &dur,
		N:        1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["duration"] != dur {
		t.Errorf("expected duration %v, got %v", dur, ts.createBody["duration"])
	}
}

func TestBuildRequestBody_Seed(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	seed := 42
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test prompt",
		Seed:   &seed,
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JSON numbers decode as float64
	if ts.createBody["seed"] != float64(42) {
		t.Errorf("expected seed 42, got %v", ts.createBody["seed"])
	}
}

func TestBuildRequestBody_Resolution_Mapped(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:     "test prompt",
		Resolution: "1920x1080",
		N:          1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["resolution"] != "1080p" {
		t.Errorf("expected resolution '1080p', got %v", ts.createBody["resolution"])
	}
}

func TestBuildRequestBody_Resolution_720p(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:     "test prompt",
		Resolution: "1280x720",
		N:          1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["resolution"] != "720p" {
		t.Errorf("expected resolution '720p', got %v", ts.createBody["resolution"])
	}
}

func TestBuildRequestBody_Resolution_480p(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:     "test prompt",
		Resolution: "864x480",
		N:          1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["resolution"] != "480p" {
		t.Errorf("expected resolution '480p', got %v", ts.createBody["resolution"])
	}
}

func TestBuildRequestBody_Resolution_Passthrough(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:     "test prompt",
		Resolution: "640x480", // not in the resolution map
		N:          1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["resolution"] != "640x480" {
		t.Errorf("expected resolution '640x480', got %v", ts.createBody["resolution"])
	}
}

// ─── Provider Options Tests ───────────────────────────────────────────────────

func TestProviderOptions_Watermark(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"watermark":      true,
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["watermark"] != true {
		t.Errorf("expected watermark true, got %v", ts.createBody["watermark"])
	}
}

func TestProviderOptions_GenerateAudio(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"generateAudio":  true,
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["generate_audio"] != true {
		t.Errorf("expected generate_audio true, got %v", ts.createBody["generate_audio"])
	}
}

func TestProviderOptions_CameraFixed(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"cameraFixed":    true,
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["camera_fixed"] != true {
		t.Errorf("expected camera_fixed true, got %v", ts.createBody["camera_fixed"])
	}
}

func TestProviderOptions_ReturnLastFrame(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"returnLastFrame": true,
				"pollIntervalMs":  10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["return_last_frame"] != true {
		t.Errorf("expected return_last_frame true, got %v", ts.createBody["return_last_frame"])
	}
}

func TestProviderOptions_ServiceTier(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"serviceTier":    "flex",
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["service_tier"] != "flex" {
		t.Errorf("expected service_tier 'flex', got %v", ts.createBody["service_tier"])
	}
}

func TestProviderOptions_Draft(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-5-pro-251215")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"draft":          true,
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["draft"] != true {
		t.Errorf("expected draft true, got %v", ts.createBody["draft"])
	}
}

func TestProviderOptions_LastFrameImage(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-5-pro-251215")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/first-frame.png",
		},
		N: 1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"lastFrameImage": "https://example.com/last-frame.png",
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := ts.createBody["content"].([]interface{})
	if len(content) != 3 {
		t.Fatalf("expected 3 content items (text, image, last_frame), got %d", len(content))
	}

	lastFrame := content[2].(map[string]interface{})
	if lastFrame["role"] != "last_frame" {
		t.Errorf("expected role 'last_frame', got %v", lastFrame["role"])
	}
	imageURL := lastFrame["image_url"].(map[string]interface{})
	if imageURL["url"] != "https://example.com/last-frame.png" {
		t.Errorf("expected last frame URL, got %v", imageURL["url"])
	}
}

func TestProviderOptions_ReferenceImages(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-lite-i2v-250428")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"referenceImages": []interface{}{
					"https://example.com/ref1.png",
					"https://example.com/ref2.png",
				},
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := ts.createBody["content"].([]interface{})
	// 1 text + 2 reference images
	if len(content) != 3 {
		t.Fatalf("expected 3 content items, got %d", len(content))
	}

	for i, refURL := range []string{"https://example.com/ref1.png", "https://example.com/ref2.png"} {
		item := content[i+1].(map[string]interface{})
		if item["role"] != "reference_image" {
			t.Errorf("item %d: expected role 'reference_image', got %v", i+1, item["role"])
		}
		imgURL := item["image_url"].(map[string]interface{})
		if imgURL["url"] != refURL {
			t.Errorf("item %d: expected URL %s, got %v", i+1, refURL, imgURL["url"])
		}
	}
}

func TestProviderOptions_Passthrough(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"custom_param":   "custom_value",
				"another_param":  123,
				"pollIntervalMs": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ts.createBody["custom_param"] != "custom_value" {
		t.Errorf("expected custom_param 'custom_value', got %v", ts.createBody["custom_param"])
	}
	if ts.createBody["another_param"] != float64(123) {
		t.Errorf("expected another_param 123, got %v", ts.createBody["another_param"])
	}
}

// ─── Image-to-Video Tests ─────────────────────────────────────────────────────

func TestImageToVideo_URLImage(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "animate this image",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/input.png",
		},
		N: 1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := ts.createBody["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected 2 content items (text + image), got %d", len(content))
	}

	imageItem := content[1].(map[string]interface{})
	if imageItem["type"] != "image_url" {
		t.Errorf("expected type 'image_url', got %v", imageItem["type"])
	}
	imageURL := imageItem["image_url"].(map[string]interface{})
	if imageURL["url"] != "https://example.com/input.png" {
		t.Errorf("expected image URL, got %v", imageURL["url"])
	}
}

func TestImageToVideo_FileImage(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	// PNG magic bytes
	imageData := []byte{137, 80, 78, 71}

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "animate this image",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      imageData,
			MediaType: "image/png",
		},
		N: 1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := ts.createBody["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(content))
	}

	imageItem := content[1].(map[string]interface{})
	imageURL := imageItem["image_url"].(map[string]interface{})
	url := imageURL["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("expected data URI for file image, got %v", url)
	}
}

// ─── Response Parsing Tests ───────────────────────────────────────────────────

func TestResponse_VideoURL(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{
		makeSuccessStatusBody("task-123", "https://bytedance.cdn/files/video-output.mp4"),
	})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(result.Videos))
	}
	if result.Videos[0].Type != "url" {
		t.Errorf("expected type 'url', got %s", result.Videos[0].Type)
	}
	if result.Videos[0].URL != "https://bytedance.cdn/files/video-output.mp4" {
		t.Errorf("expected video URL, got %s", result.Videos[0].URL)
	}
	if result.Videos[0].MediaType != "video/mp4" {
		t.Errorf("expected media type 'video/mp4', got %s", result.Videos[0].MediaType)
	}
}

func TestResponse_ProviderMetadata(t *testing.T) {
	ts := newTestServer(t, "test-task-id-123", []string{
		makeSuccessStatusBody("test-task-id-123", "https://bytedance.cdn/files/video.mp4"),
	})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, ok := result.ProviderMetadata["bytedance"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected bytedance metadata map, got %T", result.ProviderMetadata["bytedance"])
	}
	if meta["taskId"] != "test-task-id-123" {
		t.Errorf("expected taskId 'test-task-id-123', got %v", meta["taskId"])
	}
}

func TestResponse_EmptyWarnings(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", result.Warnings)
	}
}

// ─── Warning Tests ────────────────────────────────────────────────────────────

func TestWarnings_FPS(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	fps := 30
	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		FPS:    &fps,
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported" && strings.Contains(w.Message, "FPS") {
			found = true
		}
	}
	if !found {
		t.Error("expected FPS unsupported warning")
	}
}

func TestWarnings_MultipleVideos(t *testing.T) {
	ts := newTestServer(t, "task-123", []string{makeSuccessStatusBody("task-123", "https://cdn.example.com/video.mp4")})
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      3,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported" && strings.Contains(w.Message, "multiple videos") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'multiple videos' unsupported warning")
	}
}

// ─── Polling Tests ────────────────────────────────────────────────────────────

func TestPolling_PollsUntilSuccess(t *testing.T) {
	pollCount := 0
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "poll-test-id"}`)
	})

	mux.HandleFunc("/api/v3/contents/generations/tasks/poll-test-id", func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		w.Header().Set("Content-Type", "application/json")

		if pollCount < 3 {
			fmt.Fprintln(w, `{"id": "poll-test-id", "status": "processing"}`)
		} else {
			fmt.Fprintln(w, `{"id": "poll-test-id", "status": "succeeded", "content": {"video_url": "https://cdn.example.com/final.mp4"}, "usage": {"completion_tokens": 100}}`)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	result, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pollCount != 3 {
		t.Errorf("expected 3 poll calls, got %d", pollCount)
	}

	if result.Videos[0].URL != "https://cdn.example.com/final.mp4" {
		t.Errorf("expected video URL, got %s", result.Videos[0].URL)
	}
}

func TestPolling_TimeoutError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "timeout-test-id"}`)
	})

	mux.HandleFunc("/api/v3/contents/generations/tasks/timeout-test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "timeout-test-id", "status": "processing"}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{
				"pollIntervalMs": 10,
				"pollTimeoutMs":  50,
			},
		},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error message, got: %v", err)
	}
}

func TestPolling_ContextCancellation(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "abort-test-id"}`)
	})

	mux.HandleFunc("/api/v3/contents/generations/tasks/abort-test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "abort-test-id", "status": "processing"}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if !strings.Contains(err.Error(), "aborted") && !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "context") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}

// ─── Error Handling Tests ─────────────────────────────────────────────────────

func TestError_NoTaskID(t *testing.T) {
	ts := newTestServer(t, "", []string{}) // empty task ID
	defer ts.server.Close()

	prov := providerForServer(t, ts.server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err == nil {
		t.Fatal("expected error when no task ID returned")
	}
	if !strings.Contains(err.Error(), "No task ID") {
		t.Errorf("expected 'No task ID' in error, got: %v", err)
	}
}

func TestError_TaskFailed(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "failed-task-id"}`)
	})

	mux.HandleFunc("/api/v3/contents/generations/tasks/failed-task-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "failed-task-id", "status": "failed"}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err == nil {
		t.Fatal("expected error when task fails")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected 'failed' in error message, got: %v", err)
	}
}

func TestError_NoVideoURL(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id": "no-video-task-id"}`)
	})

	mux.HandleFunc("/api/v3/contents/generations/tasks/no-video-task-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// succeeded but no video_url
		fmt.Fprintln(w, `{"id": "no-video-task-id", "status": "succeeded", "content": {}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"pollIntervalMs": 10},
		},
	})
	if err == nil {
		t.Fatal("expected error when no video URL in response")
	}
	if !strings.Contains(err.Error(), "No video URL") {
		t.Errorf("expected 'No video URL' in error, got: %v", err)
	}
}

func TestError_APIError(t *testing.T) {
	server := newErrorServer(t, 400, `{"error": {"message": "Invalid prompt"}}`)
	defer server.Close()

	prov := providerForServer(t, server.URL)
	model := newVideoModel(prov, "seedance-1-0-pro-250528")

	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "test",
		N:      1,
	})
	if err == nil {
		t.Fatal("expected API error")
	}

	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.Code != 400 {
		t.Errorf("expected status code 400, got %d", apiErr.Code)
	}
}

// ─── Integration Test ─────────────────────────────────────────────────────────

func TestIntegration_ByteDanceVideoGeneration(t *testing.T) {
	if os.Getenv("BYTEDANCE_API_KEY") == "" {
		t.Skip("Skipping: BYTEDANCE_API_KEY not set")
	}

	prov, err := New(Config{})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	model, err := prov.VideoModel(string(ModelSeedance10Pro))
	if err != nil {
		t.Fatalf("failed to get video model: %v", err)
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A cherry blossom tree in the wind",
		AspectRatio: "16:9",
		N:           1,
	})
	if err != nil {
		t.Fatalf("video generation failed: %v", err)
	}

	if len(result.Videos) == 0 {
		t.Fatal("expected at least one video in result")
	}
	if result.Videos[0].URL == "" {
		t.Error("expected non-empty video URL")
	}
	if result.Videos[0].MediaType != "video/mp4" {
		t.Errorf("expected media type 'video/mp4', got %s", result.Videos[0].MediaType)
	}

	t.Logf("Generated video URL: %s", result.Videos[0].URL)
}

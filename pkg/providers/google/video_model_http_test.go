package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVideoModel_DoGenerate_Success(t *testing.T) {
	t.Parallel()

	videoBytes := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}
	pollCount := 0
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/models/veo-3.0-generate-preview:generateVideo":
			_, _ = w.Write([]byte(`{"name":"operations/test-op"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/operations/test-op":
			pollCount++
			if pollCount == 1 {
				_, _ = w.Write([]byte(`{"name":"operations/test-op","done":false}`))
				return
			}
			resp := map[string]interface{}{
				"name": "operations/test-op",
				"done": true,
				"response": map[string]interface{}{
					"generatedVideos": []map[string]interface{}{
						{"videoUri": serverURL + "/video.mp4"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodGet && r.URL.Path == "/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(videoBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	m := NewVideoModel(p, "veo-3.0-generate-preview")

	res, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A calm ocean at sunrise",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"pollIntervalMs": 1,
				"pollTimeoutMs":  500,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(res.Videos) != 1 {
		t.Fatalf("videos length = %d, want 1", len(res.Videos))
	}
	if res.Videos[0].Type != "binary" {
		t.Fatalf("video type = %q, want binary", res.Videos[0].Type)
	}
	if res.Videos[0].MediaType != "video/mp4" {
		t.Fatalf("media type = %q, want video/mp4", res.Videos[0].MediaType)
	}
	if len(res.Videos[0].Binary) == 0 {
		t.Fatal("video binary must not be empty")
	}
	if res.Response.ModelID != "veo-3.0-generate-preview" {
		t.Fatalf("response model id = %q", res.Response.ModelID)
	}
}

func TestVideoModel_DoGenerate_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("submit non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad submit", http.StatusBadRequest)
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewVideoModel(p, "veo")
		_, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 100}},
		})
		if err == nil || !strings.Contains(err.Error(), "API returned status 400") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing operation name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"done":false}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewVideoModel(p, "veo")
		_, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{})
		if err == nil || !strings.Contains(err.Error(), "no operation name") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("operation failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/models/veo:generateVideo":
				_, _ = w.Write([]byte(`{"name":"operations/fail-op"}`))
			case "/operations/fail-op":
				_, _ = w.Write([]byte(`{"name":"operations/fail-op","done":true,"error":{"code":13,"message":"denied"}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewVideoModel(p, "veo")
		_, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 200}},
		})
		if err == nil || !strings.Contains(err.Error(), "polling failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("operation success but empty videos", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/models/veo:generateVideo":
				_, _ = w.Write([]byte(`{"name":"operations/empty-op"}`))
			case "/operations/empty-op":
				_, _ = w.Write([]byte(`{"name":"operations/empty-op","done":true,"response":{"generatedVideos":[]}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewVideoModel(p, "veo")
		_, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 200}},
		})
		if err == nil || !strings.Contains(err.Error(), "no videos in response") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestVideoModel_DownloadVideo_DefaultTypeOnUnknown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0x01, 0x02, 0x03, 0x04})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key"})
	m := NewVideoModel(p, "veo")
	data, mediaType, err := m.downloadVideo(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("downloadVideo() error = %v", err)
	}
	if len(data) != 4 {
		t.Fatalf("downloaded bytes = %d, want 4", len(data))
	}
	if mediaType != "video/mp4" {
		t.Fatalf("mediaType = %q, want video/mp4 default", mediaType)
	}
}

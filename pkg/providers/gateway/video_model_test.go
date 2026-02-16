package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func TestVideoModel_SpecificationVersion(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")
	if got := model.SpecificationVersion(); got != "v3" {
		t.Errorf("SpecificationVersion() = %v, want %v", got, "v3")
	}
}

func TestVideoModel_Provider(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")
	if got := model.Provider(); got != "gateway" {
		t.Errorf("Provider() = %v, want %v", got, "gateway")
	}
}

func TestVideoModel_ModelID(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	modelID := "google/veo-3.1"
	model := NewVideoModel(p, modelID)
	if got := model.ModelID(); got != modelID {
		t.Errorf("ModelID() = %v, want %v", got, modelID)
	}
}

func TestVideoModel_MaxVideosPerCall(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")
	maxVideos := model.MaxVideosPerCall()
	if maxVideos == nil {
		t.Error("MaxVideosPerCall() returned nil")
		return
	}
	if *maxVideos != 9007199254740991 {
		t.Errorf("MaxVideosPerCall() = %v, want %v", *maxVideos, 9007199254740991)
	}
}

func TestVideoModel_DoGenerate(t *testing.T) {
	tests := []struct {
		name           string
		opts           *provider.VideoModelV3CallOptions
		serverResponse interface{}
		serverStatus   int
		wantErr        bool
		wantVideos     int
	}{
		{
			name: "successful video generation",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:      "A cat playing",
				N:           1,
				AspectRatio: "16:9",
				Duration:    floatPtr(4.0),
			},
			serverResponse: map[string]interface{}{
				"videos": []map[string]interface{}{
					{
						"type":      "url",
						"url":       "https://example.com/video.mp4",
						"mediaType": "video/mp4",
					},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
			wantVideos:   1,
		},
		{
			name: "video generation with warnings",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:   "A cat playing",
				N:        1,
				Duration: floatPtr(10.0),
			},
			serverResponse: map[string]interface{}{
				"videos": []map[string]interface{}{
					{
						"type":      "url",
						"url":       "https://example.com/video.mp4",
						"mediaType": "video/mp4",
					},
				},
				"warnings": []map[string]interface{}{
					{
						"type":    "unsupported",
						"feature": "duration",
						"details": "Duration capped at 8 seconds",
					},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
			wantVideos:   1,
		},
		{
			name: "server error",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "A cat playing",
			},
			serverResponse: map[string]interface{}{
				"error": "Internal server error",
			},
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
			wantVideos:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				// Verify path
				if r.URL.Path != "/video-model" {
					t.Errorf("Expected path /video-model, got %s", r.URL.Path)
				}

				// Verify headers
				if r.Header.Get("ai-video-model-specification-version") != "3" {
					t.Errorf("Missing or incorrect specification version header")
				}

				// Write response
				w.WriteHeader(tt.serverStatus)
				_ = json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			// Create provider with test server
			p, err := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			model := NewVideoModel(p, "google/veo-3.1")
			ctx := context.Background()

			// Call DoGenerate
			result, err := model.DoGenerate(ctx, tt.opts)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("DoGenerate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify result
			if result == nil {
				t.Fatal("DoGenerate() returned nil result")
			}

			if len(result.Videos) != tt.wantVideos {
				t.Errorf("DoGenerate() returned %d videos, want %d", len(result.Videos), tt.wantVideos)
			}
		})
	}
}

func TestVideoModel_DoGenerate_Timeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"videos": []map[string]interface{}{
				{
					"type":      "url",
					"url":       "https://example.com/video.mp4",
					"mediaType": "video/mp4",
				},
			},
		})
	}))
	defer server.Close()

	// Create provider with test server
	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat playing",
	}

	// Call DoGenerate - should timeout
	_, err = model.DoGenerate(ctx, opts)

	// Should return a timeout error
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	// Verify it's a gateway timeout error
	if !gatewayerrors.IsGatewayTimeoutError(err) {
		t.Errorf("Expected GatewayTimeoutError, got %T: %v", err, err)
	}
}

func TestVideoModel_EncodeVideoFile(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")

	tests := []struct {
		name    string
		file    *provider.VideoModelV3File
		wantErr bool
	}{
		{
			name: "URL file",
			file: &provider.VideoModelV3File{
				Type: "url",
				URL:  "https://example.com/image.jpg",
			},
			wantErr: false,
		},
		{
			name: "binary file",
			file: &provider.VideoModelV3File{
				Type:      "file",
				Data:      []byte("test data"),
				MediaType: "image/jpeg",
			},
			wantErr: false,
		},
		{
			name: "invalid file - no data",
			file: &provider.VideoModelV3File{
				Type: "file",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := model.encodeVideoFile(tt.file)
			if (err != nil) != tt.wantErr {
				t.Errorf("encodeVideoFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result == nil {
				t.Error("encodeVideoFile() returned nil result")
			}
		})
	}
}

func TestProvider_VideoModel(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		name    string
		modelID string
		wantErr bool
	}{
		{
			name:    "valid model ID",
			modelID: "google/veo-3.1",
			wantErr: false,
		},
		{
			name:    "empty model ID",
			modelID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := provider.VideoModel(tt.modelID)
			if (err != nil) != tt.wantErr {
				t.Errorf("VideoModel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && model == nil {
				t.Error("VideoModel() returned nil model")
			}
			if !tt.wantErr && model.ModelID() != tt.modelID {
				t.Errorf("VideoModel().ModelID() = %v, want %v", model.ModelID(), tt.modelID)
			}
		})
	}
}

// Helper function to create a float pointer
func floatPtr(f float64) *float64 {
	return &f
}

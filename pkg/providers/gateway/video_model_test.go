package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func TestVideoModel_SpecificationVersion(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")
	if got := model.SpecificationVersion(); got != "v4" {
		t.Errorf("SpecificationVersion() = %v, want %v", got, "v4")
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

func TestVideoModel_DoGenerate_SSE(t *testing.T) {
	tests := []struct {
		name         string
		opts         *provider.VideoModelV3CallOptions
		sseEvents    []string // raw SSE lines to write before the result event
		resultEvent  string   // the final data payload (type=result or type=error)
		serverStatus int
		wantErr      bool
		wantVideos   int
		wantWarnings int
		checkAccept  bool // whether to verify Accept: text/event-stream header
	}{
		{
			name: "successful video generation via SSE",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:      "A cat playing",
				N:           1,
				AspectRatio: "16:9",
				Duration:    floatPtr(4.0),
			},
			resultEvent:  `{"type":"result","videos":[{"type":"url","url":"https://example.com/video.mp4","mediaType":"video/mp4"}]}`,
			serverStatus: http.StatusOK,
			wantErr:      false,
			wantVideos:   1,
			checkAccept:  true,
		},
		{
			name: "SSE with heartbeat events before result errors like TS schema validation",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "A dog running",
			},
			sseEvents: []string{
				`event: heartbeat
data: {"type":"heartbeat","timestamp":1234567890}

`,
				`event: heartbeat
data: {"type":"heartbeat","timestamp":1234567891}

`,
			},
			resultEvent:  `{"type":"result","videos":[{"type":"url","url":"https://example.com/video2.mp4","mediaType":"video/mp4"}]}`,
			serverStatus: http.StatusOK,
			wantErr:      true,
		},
		{
			name: "SSE with progress events before result errors like TS schema validation",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "A landscape",
			},
			sseEvents: []string{
				"event: progress\ndata: {\"type\":\"progress\",\"percent\":25}\n\n",
				"event: progress\ndata: {\"type\":\"progress\",\"percent\":75}\n\n",
			},
			resultEvent:  `{"type":"result","videos":[{"type":"url","url":"https://example.com/landscape.mp4","mediaType":"video/mp4"}]}`,
			serverStatus: http.StatusOK,
			wantErr:      true,
		},
		{
			name: "SSE with heartbeat, progress, then result errors on first event like TS",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "Ocean waves",
			},
			sseEvents: []string{
				"event: heartbeat\ndata: {\"type\":\"heartbeat\",\"timestamp\":1111}\n\n",
				"event: progress\ndata: {\"type\":\"progress\",\"percent\":50}\n\n",
				"event: heartbeat\ndata: {\"type\":\"heartbeat\",\"timestamp\":2222}\n\n",
			},
			resultEvent:  `{"type":"result","videos":[{"type":"url","url":"https://example.com/ocean.mp4","mediaType":"video/mp4"},{"type":"url","url":"https://example.com/ocean2.mp4","mediaType":"video/mp4"}]}`,
			serverStatus: http.StatusOK,
			wantErr:      true,
		},
		{
			name: "SSE with deprecated warnings in result",
			opts: &provider.VideoModelV3CallOptions{
				Prompt:   "A cat playing",
				Duration: floatPtr(10.0),
			},
			resultEvent:  `{"type":"result","videos":[{"type":"url","url":"https://example.com/video.mp4","mediaType":"video/mp4"}],"warnings":[{"type":"deprecated","setting":"duration","message":"Use seconds instead"}]}`,
			serverStatus: http.StatusOK,
			wantErr:      false,
			wantVideos:   1,
			wantWarnings: 1,
		},
		{
			name: "SSE error event returns error",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "An invalid prompt",
			},
			resultEvent:  `{"type":"error","message":"prompt violates content policy","errorType":"content_policy","statusCode":400}`,
			serverStatus: http.StatusOK,
			wantErr:      true,
		},
		{
			name: "HTTP server error (non-SSE path)",
			opts: &provider.VideoModelV3CallOptions{
				Prompt: "A cat playing",
			},
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and path
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST request, got %s", r.Method)
				}
				if r.URL.Path != "/video-model" {
					t.Errorf("Expected path /video-model, got %s", r.URL.Path)
				}

				// Verify spec version header and model ID header
				if r.Header.Get("ai-video-model-specification-version") != "4" {
					t.Errorf("Missing or incorrect specification version header")
				}
				if r.Header.Get("ai-model-id") == "" {
					t.Errorf("Missing ai-model-id header")
				}

				// Verify Accept: text/event-stream header
				if tt.checkAccept && r.Header.Get("Accept") != "text/event-stream" {
					t.Errorf("Expected Accept: text/event-stream, got %s", r.Header.Get("Accept"))
				}

				// For error status codes, just write the status and return
				if tt.serverStatus >= 400 {
					w.WriteHeader(tt.serverStatus)
					return
				}

				// Set SSE content type
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)

				// Write any pre-result SSE events
				for _, ev := range tt.sseEvents {
					_, _ = fmt.Fprint(w, ev) //nolint:errcheck
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}

				// Write the result/error event
				if tt.resultEvent != "" {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", tt.resultEvent) //nolint:errcheck
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}))
			defer server.Close()

			p, err := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			model := NewVideoModel(p, "google/veo-3.1")
			ctx := context.Background()

			result, err := model.DoGenerate(ctx, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Errorf("DoGenerate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if result == nil {
				t.Fatal("DoGenerate() returned nil result")
			}
			if len(result.Videos) != tt.wantVideos {
				t.Errorf("DoGenerate() returned %d videos, want %d", len(result.Videos), tt.wantVideos)
			}
			if len(result.Warnings) != tt.wantWarnings {
				t.Errorf("DoGenerate() returned %d warnings, want %d", len(result.Warnings), tt.wantWarnings)
			}
			if tt.name == "SSE with deprecated warnings in result" {
				if result.Warnings[0].Type != "deprecated" || result.Warnings[0].Setting != "duration" || result.Warnings[0].Message != "Use seconds instead" {
					t.Fatalf("warning fields should match TS shape, got %#v", result.Warnings[0])
				}
			}
		})
	}
}

func TestVideoModel_DoGenerate_SSE_ContextCancellation(t *testing.T) {
	// The server sends heartbeats then blocks before the result
	serverReady := make(chan struct{})
	serverDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send one heartbeat
		_, _ = fmt.Fprint(w, "event: heartbeat\ndata: {\"type\":\"heartbeat\",\"timestamp\":1234}\n\n") //nolint:errcheck
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Signal that we've sent the heartbeat
		close(serverReady)

		// Block until the test signals us to stop (simulates long generation)
		<-serverDone
	}))
	defer server.Close()
	defer close(serverDone)

	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")

	ctx, cancel := context.WithCancel(context.Background())

	opts := &provider.VideoModelV3CallOptions{
		Prompt: "A cat playing",
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := model.DoGenerate(ctx, opts)
		errCh <- err
	}()

	// Wait until the server has sent the heartbeat, then cancel
	<-serverReady
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Expected error after context cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Error("DoGenerate did not return after context cancellation")
	}
}

func TestVideoModel_DoGenerate_SSE_StreamEndedWithoutCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Send only heartbeats then close without result
		_, _ = fmt.Fprint(w, "event: heartbeat\ndata: {\"type\":\"heartbeat\",\"timestamp\":1234}\n\n") //nolint:errcheck
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Connection closes here without a result or error event
	}))
	defer server.Close()

	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")
	_, err = model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A cat",
	})

	if err == nil {
		t.Error("Expected error when SSE stream ends without completion, got nil")
	}
}

func TestVideoModel_DoGenerate_Timeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"result","videos":[{"type":"url","url":"https://example.com/video.mp4","mediaType":"video/mp4"}]}`) //nolint:errcheck
	}))
	defer server.Close()

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

	// Call DoGenerate — should timeout
	_, err = model.DoGenerate(ctx, opts)

	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	// Verify it's a gateway timeout error
	if !gatewayerrors.IsGatewayTimeoutError(err) {
		t.Errorf("Expected GatewayTimeoutError, got %T: %v", err, err)
	}
}

func TestVideoModel_DoGenerate_AcceptHeader(t *testing.T) {
	var capturedAccept string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"result","videos":[{"type":"url","url":"https://example.com/video.mp4","mediaType":"video/mp4"}]}`) //nolint:errcheck
	}))
	defer server.Close()

	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.1")
	_, err = model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A cat",
	})
	if err != nil {
		t.Fatalf("DoGenerate() unexpected error: %v", err)
	}

	if capturedAccept != "text/event-stream" {
		t.Errorf("Expected Accept: text/event-stream, got: %q", capturedAccept)
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
		assert  func(t *testing.T, result interface{})
	}{
		{
			name: "URL file",
			file: &provider.VideoModelV3File{
				Type:      "url",
				URL:       "https://example.com/image.jpg",
				MediaType: "image/jpeg",
			},
			wantErr: false,
			assert: func(t *testing.T, result interface{}) {
				got := result.(map[string]interface{})
				if got["type"] != "url" || got["url"] != "https://example.com/image.jpg" || got["mediaType"] != "image/jpeg" {
					t.Fatalf("url file = %#v", got)
				}
			},
		},
		{
			name: "binary file",
			file: &provider.VideoModelV3File{
				Type:      "file",
				Data:      []byte("test data"),
				MediaType: "image/jpeg",
			},
			wantErr: false,
			assert: func(t *testing.T, result interface{}) {
				got := result.(map[string]interface{})
				if got["type"] != "file" || got["data"] != "dGVzdCBkYXRh" || got["mediaType"] != "image/jpeg" {
					t.Fatalf("binary file = %#v", got)
				}
			},
		},
		{
			name: "empty binary file preserves empty base64 data and omits default media type",
			file: &provider.VideoModelV3File{
				Type: "file",
				Data: []byte{},
			},
			wantErr: false,
			assert: func(t *testing.T, result interface{}) {
				got := result.(map[string]interface{})
				if got["type"] != "file" || got["data"] != "" {
					t.Fatalf("empty binary file = %#v", got)
				}
				if _, ok := got["mediaType"]; ok {
					t.Fatalf("mediaType should not be defaulted when absent, got %#v", got)
				}
			},
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
			if !tt.wantErr && tt.assert != nil {
				tt.assert(t, result)
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

// TestGatewayVideoGenerationSSE_Integration is an integration test that runs against
// the real AI Gateway. It is skipped when GATEWAY_API_KEY is not set.
func TestGatewayVideoGenerationSSE_Integration(t *testing.T) {
	apiKey := os.Getenv("GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: GATEWAY_API_KEY not set")
	}

	p, err := New(Config{APIKey: apiKey})
	if err != nil {
		t.Fatalf("Failed to create gateway provider: %v", err)
	}

	model := NewVideoModel(p, "google/veo-3.0-generate-preview")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A calm ocean wave at sunset",
		N:           1,
		AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("DoGenerate() integration error: %v", err)
	}

	if result == nil {
		t.Fatal("DoGenerate() returned nil result")
	}
	if len(result.Videos) == 0 {
		t.Error("DoGenerate() returned no videos")
	}
	for i, v := range result.Videos {
		if v.URL == "" && v.Data == "" {
			t.Errorf("Video[%d] has neither URL nor Data", i)
		}
	}
}

// TestVideoModel_ModelIDHeader confirms the shared ai-model-id header is sent.
func TestVideoModel_ModelIDHeader(t *testing.T) {
	var capturedModelID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedModelID = r.Header.Get("ai-model-id")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"result","videos":[{"type":"url","url":"https://example.com/v.mp4","mediaType":"video/mp4"}]}`)
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	modelID := "google/veo-3.1"
	model := NewVideoModel(p, modelID)
	_, err = model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "A cat",
	})
	if err != nil {
		t.Fatalf("DoGenerate() unexpected error: %v", err)
	}

	if capturedModelID != modelID {
		t.Errorf("Expected ai-model-id header %q, got %q", modelID, capturedModelID)
	}
}

// TestVideoModel_ProviderOptionsWireParity confirms providerOptions follows the TS spread behavior.
func TestVideoModel_ProviderOptionsWireParity(t *testing.T) {
	tests := []struct {
		name            string
		providerOptions map[string]interface{}
		wantPresent     bool
	}{
		{
			name:            "empty provider options still sent",
			providerOptions: map[string]interface{}{},
			wantPresent:     true,
		},
		{
			name:            "nil provider options omitted",
			providerOptions: nil,
			wantPresent:     false,
		},
		{
			name: "non-empty provider options sent",
			providerOptions: map[string]interface{}{
				"fal": map[string]interface{}{"loop": true},
			},
			wantPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
					t.Errorf("Failed to decode request body: %v", err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"result","videos":[{"type":"url","url":"https://example.com/v.mp4","mediaType":"video/mp4"}]}`)
			}))
			defer server.Close()

			p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			model := NewVideoModel(p, "google/veo-3.1")
			_, err = model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
				Prompt:          "A cat",
				ProviderOptions: tt.providerOptions,
			})
			if err != nil {
				t.Fatalf("DoGenerate() error: %v", err)
			}

			_, ok := capturedBody["providerOptions"]
			if ok != tt.wantPresent {
				t.Errorf("providerOptions presence = %v, want %v. body: %v", ok, tt.wantPresent, capturedBody)
			}
		})
	}
}

func TestVideoModel_ZeroNumericOptionsWireParity(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"result","videos":[{"type":"url","url":"https://example.com/v.mp4","mediaType":"video/mp4"}]}`)
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	zeroFloat := 0.0
	zeroInt := 0
	_, err = NewVideoModel(p, "google/veo-3.1").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:   "",
		N:        0,
		Duration: &zeroFloat,
		FPS:      &zeroInt,
		Seed:     &zeroInt,
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if capturedBody["prompt"] != "" || capturedBody["n"] != float64(0) {
		t.Fatalf("prompt and n should be sent unconditionally like TS: %v", capturedBody)
	}
	for _, key := range []string{"duration", "fps", "seed"} {
		if _, ok := capturedBody[key]; ok {
			t.Fatalf("%s=0 should be omitted to match TS truthy spread behavior: %v", key, capturedBody)
		}
	}
}

// TestVideoModel_URLImageEncoding confirms URL-type images are sent as full objects.
func TestVideoModel_URLImageEncoding(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"result","videos":[{"type":"url","url":"https://example.com/v.mp4","mediaType":"video/mp4"}]}`)
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	model := NewVideoModel(p, "google/veo-3.1")
	_, err = model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "Animate this",
		Image: &provider.VideoModelV3File{
			Type: "url",
			URL:  "https://example.com/image.png",
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	imageRaw, ok := capturedBody["image"]
	if !ok {
		t.Fatal("Expected 'image' key in request body, not found")
	}
	imageMap, ok := imageRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected image to be object, got %T: %v", imageRaw, imageRaw)
	}
	if imageMap["type"] != "url" {
		t.Errorf("Expected image.type = %q, got %v", "url", imageMap["type"])
	}
	if imageMap["url"] != "https://example.com/image.png" {
		t.Errorf("Expected image.url = %q, got %v", "https://example.com/image.png", imageMap["url"])
	}
}

// TestVideoModel_SSEErrorEventStructured confirms SSE error events produce a ProviderError
// with statusCode and errorType extracted from the event.
func TestVideoModel_SSEErrorEventStructured(t *testing.T) {
	tests := []struct {
		name           string
		sseError       string
		wantStatusCode int
		wantErrorCode  string
		wantMsg        string
	}{
		{
			name:           "rate limit error with status code",
			sseError:       `{"type":"error","message":"Rate limit exceeded","errorType":"rate_limit_exceeded","statusCode":429,"param":null}`,
			wantStatusCode: 429,
			wantErrorCode:  "rate_limit_exceeded",
			wantMsg:        "Rate limit exceeded",
		},
		{
			name:           "internal server error",
			sseError:       `{"type":"error","message":"All providers failed","errorType":"internal_server_error","statusCode":500,"param":null}`,
			wantStatusCode: 500,
			wantErrorCode:  "internal_server_error",
			wantMsg:        "All providers failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", tt.sseError)
			}))
			defer server.Close()

			p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			model := NewVideoModel(p, "google/veo-3.1")

			_, err = model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
				Prompt: "A cat",
			})

			if err == nil {
				t.Fatal("Expected error from SSE error event, got nil")
			}

			// Verify the error message is included
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Expected error to contain %q, got: %v", tt.wantMsg, err)
			}

			// Verify the error is a ProviderError with the correct status code
			var provErr *providererrors.ProviderError
			if !errors.As(err, &provErr) {
				t.Fatalf("Expected ProviderError, got %T: %v", err, err)
			}
			if provErr.StatusCode != tt.wantStatusCode {
				t.Errorf("Expected StatusCode %d, got %d", tt.wantStatusCode, provErr.StatusCode)
			}
			if provErr.ErrorCode != tt.wantErrorCode {
				t.Errorf("Expected ErrorCode %q, got %q", tt.wantErrorCode, provErr.ErrorCode)
			}
		})
	}
}

// Helper function to create a float pointer
func floatPtr(f float64) *float64 {
	return &f
}

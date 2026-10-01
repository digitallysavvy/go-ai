package prodia

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestProdiaVideoModelSpecificationVersion verifies the spec version is "v4".
func TestProdiaVideoModelSpecificationVersion(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	if got := model.SpecificationVersion(); got != "v4" {
		t.Errorf("SpecificationVersion() = %q, want %q", got, "v4")
	}
}

// TestProdiaVideoModelProvider verifies the provider name matches the
// TypeScript SDK's "prodia.video" value.
func TestProdiaVideoModelProvider(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	if got := model.Provider(); got != "prodia.video" {
		t.Errorf("Provider() = %q, want %q", got, "prodia.video")
	}
}

// TestProdiaVideoModelID verifies that ModelID returns the provided ID.
func TestProdiaVideoModelID(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	for _, id := range []string{VideoModelWan22LightningTxt2Vid, VideoModelWan22LightningImg2Vid} {
		model := NewVideoModel(prov, id)
		if got := model.ModelID(); got != id {
			t.Errorf("ModelID() = %q, want %q", got, id)
		}
	}
}

// TestProdiaVideoModelMaxVideosPerCall verifies MaxVideosPerCall returns 1.
func TestProdiaVideoModelMaxVideosPerCall(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)

	n := model.MaxVideosPerCall()
	if n == nil {
		t.Fatal("MaxVideosPerCall() = nil, want *1")
	}
	if *n != 1 {
		t.Errorf("MaxVideosPerCall() = %d, want 1", *n)
	}
}

// TestProdiaVideoModelImplementsInterface verifies that ProdiaVideoModel
// satisfies the provider.VideoModelV3 interface at compile time.
func TestProdiaVideoModelImplementsInterface(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	var _ provider.VideoModelV3 = NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)
}

// TestProviderVideoModelRouting verifies VideoModel() routes wan2-2 model IDs
// correctly and rejects unknown IDs.
func TestProviderVideoModelRouting(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	// txt2vid model
	vm, err := prov.VideoModel(VideoModelWan22LightningTxt2Vid)
	if err != nil {
		t.Fatalf("VideoModel(%q) error: %v", VideoModelWan22LightningTxt2Vid, err)
	}
	if vm.ModelID() != VideoModelWan22LightningTxt2Vid {
		t.Errorf("ModelID() = %q, want %q", vm.ModelID(), VideoModelWan22LightningTxt2Vid)
	}

	// img2vid model
	vm2, err := prov.VideoModel(VideoModelWan22LightningImg2Vid)
	if err != nil {
		t.Fatalf("VideoModel(%q) error: %v", VideoModelWan22LightningImg2Vid, err)
	}
	if vm2.ModelID() != VideoModelWan22LightningImg2Vid {
		t.Errorf("ModelID() = %q, want %q", vm2.ModelID(), VideoModelWan22LightningImg2Vid)
	}

	// Empty model ID → default txt2vid
	vm3, err := prov.VideoModel("")
	if err != nil {
		t.Fatalf("VideoModel(\"\") error: %v", err)
	}
	if vm3.ModelID() != VideoModelWan22LightningTxt2Vid {
		t.Errorf("default ModelID() = %q, want %q", vm3.ModelID(), VideoModelWan22LightningTxt2Vid)
	}

	// Unknown model ID → error
	_, err = prov.VideoModel("some-other-model")
	if err == nil {
		t.Error("expected error for unsupported video model, got nil")
	}
}

// TestVideoModelConstants verifies that the model ID constants are set to the
// expected values.
func TestVideoModelConstants(t *testing.T) {
	if VideoModelWan22LightningTxt2Vid != "inference.wan2-2.lightning.txt2vid.v0" {
		t.Errorf("VideoModelWan22LightningTxt2Vid = %q, want %q",
			VideoModelWan22LightningTxt2Vid, "inference.wan2-2.lightning.txt2vid.v0")
	}
	if VideoModelWan22LightningImg2Vid != "inference.wan2-2.lightning.img2vid.v0" {
		t.Errorf("VideoModelWan22LightningImg2Vid = %q, want %q",
			VideoModelWan22LightningImg2Vid, "inference.wan2-2.lightning.img2vid.v0")
	}
}

// TestExtractVideoProviderOptionsResolution verifies resolution extraction from
// provider options.
func TestExtractVideoProviderOptionsResolution(t *testing.T) {
	opts := &provider.VideoModelV3CallOptions{
		ProviderOptions: map[string]interface{}{
			"prodia": map[string]interface{}{
				"resolution": "720p",
			},
		},
	}

	provOpts := extractVideoProviderOptions(opts)
	if provOpts == nil {
		t.Fatal("expected non-nil provider options")
	}
	if provOpts.Resolution != "720p" {
		t.Errorf("Resolution = %q, want %q", provOpts.Resolution, "720p")
	}
}

// TestExtractVideoProviderOptionsNil verifies that nil ProviderOptions returns
// nil.
func TestExtractVideoProviderOptionsNil(t *testing.T) {
	opts := &provider.VideoModelV3CallOptions{}
	if got := extractVideoProviderOptions(opts); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestProdiaVideoModelSerializesOnlyTypeScriptJobConfigFields(t *testing.T) {
	var requestBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/job" || r.URL.Query().Get("price") != "true" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		body, contentType := buildTestMultipartBody(
			`{"id":"job-vid-123","state":{"current":"completed"},"config":{"seed":42}}`,
			[]byte("video"),
			"video/mp4",
		)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)
	seed := 42
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:      "a cat running",
		AspectRatio: "10:3",
		Resolution:  "1080p",
		Seed:        &seed,
		ProviderOptions: map[string]interface{}{
			"prodia": map[string]interface{}{"resolution": "720p"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	config, ok := requestBody["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("config = %#v", requestBody["config"])
	}
	if config["prompt"] != "a cat running" || config["seed"].(float64) != 42 || config["resolution"] != "720p" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if _, ok := config["aspect_ratio"]; ok {
		t.Fatalf("aspect_ratio should not be serialized: %#v", config)
	}
}

func TestProdiaVideoModelPromptSetMatchesTypeScriptOptionalPrompt(t *testing.T) {
	tests := []struct {
		name       string
		opts       provider.VideoModelV3CallOptions
		wantPrompt *string
	}{
		{
			name: "explicit empty prompt is serialized",
			opts: provider.VideoModelV3CallOptions{
				Prompt:    "",
				PromptSet: true,
			},
			wantPrompt: prodiaStringPtr(""),
		},
		{
			name:       "omitted prompt is not serialized",
			opts:       provider.VideoModelV3CallOptions{},
			wantPrompt: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestBody map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/job" || r.URL.Query().Get("price") != "true" {
					t.Fatalf("unexpected request: %s", r.URL.String())
				}
				if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				body, contentType := buildTestMultipartBody(
					`{"id":"job-vid-123","state":{"current":"completed"}}`,
					[]byte("video"),
					"video/mp4",
				)
				w.Header().Set("Content-Type", contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			}))
			defer server.Close()

			prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
			model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)
			if _, err := model.DoGenerate(context.Background(), &tt.opts); err != nil {
				t.Fatalf("DoGenerate() error = %v", err)
			}

			config, ok := requestBody["config"].(map[string]interface{})
			if !ok {
				t.Fatalf("config = %#v", requestBody["config"])
			}
			got, exists := config["prompt"]
			if tt.wantPrompt == nil {
				if exists {
					t.Fatalf("prompt serialized unexpectedly: %#v", config)
				}
				return
			}
			if !exists || got != *tt.wantPrompt {
				t.Fatalf("prompt = %#v (exists %v), want %q", got, exists, *tt.wantPrompt)
			}
		})
	}
}

func TestProdiaVideoModelProviderErrorMatchesTypeScriptMessagePrecedence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-error")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Invalid prompt","detail":"Prompt cannot be empty"}`))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewVideoModel(prov, VideoModelWan22LightningTxt2Vid)
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:    "",
		PromptSet: true,
	})
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if providerErr.StatusCode != http.StatusBadRequest || providerErr.Message != "Prompt cannot be empty" {
		t.Fatalf("provider error = %#v", providerErr)
	}
	if providerErr.ResponseHeaders["X-Request-Id"] != "req-error" {
		t.Fatalf("response headers = %#v", providerErr.ResponseHeaders)
	}
	if providerErr.ResponseBody != `{"message":"Invalid prompt","detail":"Prompt cannot be empty"}` {
		t.Fatalf("response body = %q", providerErr.ResponseBody)
	}
}

func prodiaStringPtr(s string) *string {
	return &s
}

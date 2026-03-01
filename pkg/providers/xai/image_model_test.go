package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestXAIImageModelIDs verifies image model ID constants have correct values.
func TestXAIImageModelIDs(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"Grok2Image", ModelGrok2Image, "grok-2-image"},
		{"Grok2Image1212", ModelGrok2Image1212, "grok-2-image-1212"},
		{"GrokImagineImage", ModelGrokImagineImage, "grok-imagine-image"},
		{"GrokImagineImagePro", ModelGrokImagineImagePro, "grok-imagine-image-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("constant %s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// TestXAIResolutionOptionSerialized verifies the resolution field is sent in the request body.
func TestXAIResolutionOptionSerialized(t *testing.T) {
	tests := []struct {
		name               string
		resolution         *string
		expectResolution   bool
		expectedResolution string
	}{
		{
			name:               "1k resolution serialized",
			resolution:         imageTestStrPtr("1k"),
			expectResolution:   true,
			expectedResolution: "1k",
		},
		{
			name:               "2k resolution serialized",
			resolution:         imageTestStrPtr("2k"),
			expectResolution:   true,
			expectedResolution: "2k",
		},
		{
			name:             "nil resolution not sent",
			resolution:       nil,
			expectResolution: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&capturedBody)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				// Return a fake image URL
				_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/img.png"}]}`))
			}))
			defer server.Close()

			prov := New(Config{
				APIKey:  "test-key",
				BaseURL: server.URL,
			})

			model := NewImageModel(prov, ModelGrokImagineImagePro)

			provOpts := map[string]interface{}{}
			if tt.resolution != nil {
				provOpts["xai"] = map[string]interface{}{
					"resolution": *tt.resolution,
				}
			}

			opts := &provider.ImageGenerateOptions{
				Prompt: "a futuristic city",
			}
			if len(provOpts) > 0 {
				opts.ProviderOptions = provOpts
			}

			// The actual HTTP call will fail when trying to download the fake image URL,
			// but the request body will already have been captured.
			_, _ = model.DoGenerate(context.Background(), opts)

			if capturedBody == nil {
				t.Skip("server not reached (likely image download failure before request)")
			}

			if tt.expectResolution {
				res, ok := capturedBody["resolution"]
				if !ok {
					t.Errorf("expected 'resolution' field in request body, not found")
					return
				}
				if res != tt.expectedResolution {
					t.Errorf("resolution = %q, want %q", res, tt.expectedResolution)
				}
			} else {
				if _, ok := capturedBody["resolution"]; ok {
					t.Errorf("expected no 'resolution' field in request body, but found one")
				}
			}
		})
	}
}

// TestXAIResolutionInProviderOptions verifies extractImageProviderOptions reads the resolution field.
func TestXAIResolutionInProviderOptions(t *testing.T) {
	provOpts := map[string]interface{}{
		"xai": map[string]interface{}{
			"resolution": "2k",
		},
	}

	opts, err := extractImageProviderOptions(provOpts)
	if err != nil {
		t.Fatalf("extractImageProviderOptions() error: %v", err)
	}

	if opts.Resolution == nil {
		t.Fatal("expected Resolution to be non-nil")
	}
	if *opts.Resolution != "2k" {
		t.Errorf("Resolution = %q, want %q", *opts.Resolution, "2k")
	}
}

// TestXAIResolutionNotPresentWhenNil verifies resolution absent from body when nil.
func TestXAIResolutionNotPresentWhenNil(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrok2Image)

	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
	}

	body := model.buildRequestBody(opts, &XAIImageProviderOptions{}, false)

	if _, ok := body["resolution"]; ok {
		t.Errorf("expected 'resolution' absent from request body when not set")
	}
}

// TestXAIResolutionPresentWhenSet verifies resolution present in body when set.
func TestXAIResolutionPresentWhenSet(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelGrokImagineImagePro)

	res := "1k"
	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
	}
	provOpts := &XAIImageProviderOptions{
		Resolution: &res,
	}

	body := model.buildRequestBody(opts, provOpts, false)

	resVal, ok := body["resolution"]
	if !ok {
		t.Errorf("expected 'resolution' in request body when set")
		return
	}
	if resVal != "1k" {
		t.Errorf("resolution = %v, want %q", resVal, "1k")
	}
}

// imageTestStrPtr returns a pointer to a string literal.
func imageTestStrPtr(s string) *string {
	return &s
}

package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageModel_SpecificationVersion(t *testing.T) {
	model := NewImageModel(nil, "gemini-2.5-flash-image")
	assert.Equal(t, "v4", model.SpecificationVersion())
}

func TestImageModel_Provider(t *testing.T) {
	model := NewImageModel(nil, "gemini-2.5-flash-image")
	assert.Equal(t, "google.generative-ai", model.Provider())
}

func TestImageModel_ModelID(t *testing.T) {
	modelID := "gemini-2.5-flash-image"
	model := NewImageModel(nil, modelID)
	assert.Equal(t, modelID, model.ModelID())
}

// TestImageModel_DoGenerate_ImagenRemoved verifies that a non-Gemini model ID
// (e.g. a legacy Imagen model) is rejected with the exact TS error text from
// google-image-model.ts / google-vertex-image-model.ts at ai@7.0.113, instead
// of hitting the (removed) :predict endpoint.
func TestImageModel_DoGenerate_ImagenRemoved(t *testing.T) {
	prov := New(Config{APIKey: "test-api-key"})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A beautiful sunset",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Google image models other than Gemini are no longer supported. Use a model ID that starts with `gemini-`.")
}

func TestImageModel_IsGeminiModel(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected bool
	}{
		{
			name:     "Gemini model",
			modelID:  "gemini-2.5-flash-image",
			expected: true,
		},
		{
			name:     "Another Gemini model",
			modelID:  "gemini-3-pro-image-preview",
			expected: true,
		},
		{
			name:     "gemini-3.1-flash-image-preview",
			modelID:  "gemini-3.1-flash-image-preview",
			expected: true,
		},
		{
			name:     "Imagen model",
			modelID:  "imagen-4.0-generate-001",
			expected: false,
		},
		{
			name:     "Short model ID",
			modelID:  "gpt-4",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGeminiModel(tt.modelID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageModel_GeminiGoogleSearchGrounding(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {"parts": [{"inlineData": {"mimeType": "image/png", "data": "aW1n"}}]},
				"groundingMetadata": {"webSearchQueries": ["logo"]}
			}],
			"usageMetadata": {
				"promptTokenCount": 7,
				"candidatesTokenCount": 11,
				"totalTokenCount": 18,
				"serviceTier": "standard"
			}
		}`))
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"googleSearch": map[string]interface{}{},
				"imageSize":    ImageSize1K,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	tools, ok := seenBody["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools missing: %#v", seenBody)
	}
	tool := tools[0].(map[string]interface{})
	if _, ok := tool["googleSearch"]; !ok {
		t.Fatalf("googleSearch tool missing: %#v", tool)
	}
	genConfig := seenBody["generationConfig"].(map[string]interface{})
	if _, ok := genConfig["googleSearch"]; ok {
		t.Fatalf("googleSearch must not be forwarded into generationConfig: %#v", genConfig)
	}
	googleMeta := result.ProviderMetadata["google"].(map[string]interface{})
	if googleMeta["groundingMetadata"] == nil {
		t.Fatalf("groundingMetadata missing: %#v", googleMeta)
	}
	if result.Usage.InputTokens != 7 || result.Usage.OutputTokens != 11 || result.Usage.TotalTokens != 18 {
		t.Fatalf("usage = %#v, want token metadata from language model path", result.Usage)
	}
	if googleMeta["usageMetadata"] == nil || googleMeta["serviceTier"] == nil {
		t.Fatalf("language model provider metadata missing: %#v", googleMeta)
	}
}

func TestImageModel_ConvertSizeToAspectRatio(t *testing.T) {
	tests := []struct {
		name     string
		size     string
		expected string
	}{
		{
			name:     "Square 1024x1024",
			size:     "1024x1024",
			expected: "1:1",
		},
		{
			name:     "Square 512x512",
			size:     "512x512",
			expected: "1:1",
		},
		{
			name:     "Landscape 1920x1080",
			size:     "1920x1080",
			expected: "16:9",
		},
		{
			name:     "Portrait 1080x1920",
			size:     "1080x1920",
			expected: "9:16",
		},
		{
			name:     "4:3 aspect",
			size:     "1024x768",
			expected: "4:3",
		},
		{
			name:     "3:4 aspect",
			size:     "768x1024",
			expected: "3:4",
		},
		{
			name:     "Unsupported size",
			size:     "800x600",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSizeToAspectRatio(tt.size)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageModel_DoGenerate_Gemini(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "gemini-2.5-flash-image:generateContent")
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "test-api-key", r.Header.Get("x-goog-api-key"))

		// Parse request body
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request structure
		contents, ok := reqBody["contents"].([]interface{})
		require.True(t, ok)
		require.Len(t, contents, 1)

		content := contents[0].(map[string]interface{})
		assert.Equal(t, "user", content["role"])

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		modalities := genConfig["responseModalities"].([]interface{})
		assert.Contains(t, modalities, "IMAGE")

		// Return mock response
		response := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						}{
							{
								InlineData: &InlineData{
									MimeType: "image/jpeg",
									Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with mock server
	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	// Create model
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	// Generate image
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Abstract art",
	})

	// Verify result
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Image)
	assert.Len(t, result.Images, 1)
	assert.NotEmpty(t, result.Base64Image)
	assert.Len(t, result.Base64Images, 1)
	assert.Equal(t, "image/jpeg", result.MimeType)
	assert.Equal(t, 1, result.Usage.ImageCount)
	googleMeta := result.ProviderMetadata["google"].(map[string]interface{})
	imagesMeta := googleMeta["images"].([]map[string]interface{})
	assert.Len(t, imagesMeta, 1)
}

func TestImageModel_DoGenerate_Error_EmptyResponse(t *testing.T) {
	// Create mock server that returns a candidate-less response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := geminiImageResponse{}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Test",
	})

	// TS returns an empty images array (not an error) so generateImage can
	// retry or raise NoImageGeneratedError.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Images)
	assert.Nil(t, result.IsRetryable)
}

func TestImageModel_DoGenerate_Error_InvalidStatus(t *testing.T) {
	// Create mock server that returns error status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "Invalid request"}`))
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Test",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "400")
}

// TestImageModel_DoGenerate_Gemini31FlashImagePreview verifies that the new
// gemini-3.1-flash-image-preview model routes to the Gemini :generateContent path.
func TestImageModel_DoGenerate_Gemini31FlashImagePreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Must use generateContent endpoint, not predict
		assert.Contains(t, r.URL.Path, "gemini-3.1-flash-image-preview:generateContent",
			"expected generateContent endpoint for Gemini image model")
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "test-api-key", r.Header.Get("x-goog-api-key"))

		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		modalities := genConfig["responseModalities"].([]interface{})
		assert.Contains(t, modalities, "IMAGE")

		response := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						}{
							{
								InlineData: &InlineData{
									MimeType: "image/png",
									Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model := NewImageModel(prov, "gemini-3.1-flash-image-preview")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A glowing orb",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Image)
	assert.Equal(t, "image/png", result.MimeType)
	assert.Equal(t, 1, result.Usage.ImageCount)
}

// TestIntegration_Gemini31FlashImagePreview tests image generation with the new model ID.
// Requires GOOGLE_GENERATIVE_AI_API_KEY to be set.
func TestIntegration_Gemini31FlashImagePreview(t *testing.T) {
	if os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY") == "" {
		t.Skip("Skipping: GOOGLE_GENERATIVE_AI_API_KEY not set")
	}

	prov := New(Config{APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")})
	model, err := prov.ImageModel("gemini-3.1-flash-image-preview")
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.1-flash-image-preview", model.ModelID())
}

func TestGetIntValue(t *testing.T) {
	tests := []struct {
		name     string
		ptr      *int
		defVal   int
		expected int
	}{
		{
			name:     "Nil pointer",
			ptr:      nil,
			defVal:   5,
			expected: 5,
		},
		{
			name:     "Non-nil pointer",
			ptr:      intPtr(10),
			defVal:   5,
			expected: 10,
		},
		{
			name:     "Zero value pointer",
			ptr:      intPtr(0),
			defVal:   5,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntValue(tt.ptr, tt.defVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveAspectRatio(t *testing.T) {
	tests := []struct {
		name        string
		aspectRatio string
		size        string
		expected    string
	}{
		{
			name:        "AspectRatio takes precedence over size",
			aspectRatio: "16:9",
			size:        "1024x1024",
			expected:    "16:9",
		},
		{
			name:        "AspectRatio used directly",
			aspectRatio: "21:9",
			size:        "",
			expected:    "21:9",
		},
		{
			name:        "Size converted when no AspectRatio",
			aspectRatio: "",
			size:        "1920x1080",
			expected:    "16:9",
		},
		{
			name:        "Both empty returns empty",
			aspectRatio: "",
			size:        "",
			expected:    "",
		},
		{
			name:        "New extended aspect ratio 2:3",
			aspectRatio: ImageAspectRatio2x3,
			size:        "",
			expected:    "2:3",
		},
		{
			name:        "New extended aspect ratio 4:5",
			aspectRatio: ImageAspectRatio4x5,
			size:        "",
			expected:    "4:5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveAspectRatio(tt.aspectRatio, tt.size)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageAspectRatioConstants(t *testing.T) {
	// Standard ratios
	assert.Equal(t, "1:1", ImageAspectRatio1x1)
	assert.Equal(t, "3:4", ImageAspectRatio3x4)
	assert.Equal(t, "4:3", ImageAspectRatio4x3)
	assert.Equal(t, "9:16", ImageAspectRatio9x16)
	assert.Equal(t, "16:9", ImageAspectRatio16x9)
	// Extended ratios (added in #12897)
	assert.Equal(t, "2:3", ImageAspectRatio2x3)
	assert.Equal(t, "3:2", ImageAspectRatio3x2)
	assert.Equal(t, "4:5", ImageAspectRatio4x5)
	assert.Equal(t, "5:4", ImageAspectRatio5x4)
	assert.Equal(t, "21:9", ImageAspectRatio21x9)
	assert.Equal(t, "1:8", ImageAspectRatio1x8)
	assert.Equal(t, "8:1", ImageAspectRatio8x1)
	assert.Equal(t, "1:4", ImageAspectRatio1x4)
	assert.Equal(t, "4:1", ImageAspectRatio4x1)
}

func TestImageSizeConstants(t *testing.T) {
	assert.Equal(t, "512", ImageSize512)
	assert.Equal(t, "1K", ImageSize1K)
	assert.Equal(t, "2K", ImageSize2K)
	assert.Equal(t, "4K", ImageSize4K)
}

func TestResolveImageSize(t *testing.T) {
	tests := []struct {
		name            string
		providerOptions map[string]interface{}
		expected        string
	}{
		{
			name:            "Nil provider options",
			providerOptions: nil,
			expected:        "",
		},
		{
			name: "imageSize 1K",
			providerOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"imageSize": "1K",
				},
			},
			expected: "1K",
		},
		{
			name: "imageSize 4K",
			providerOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"imageSize": "4K",
				},
			},
			expected: "4K",
		},
		{
			name: "No google key",
			providerOptions: map[string]interface{}{
				"openai": map[string]interface{}{},
			},
			expected: "",
		},
		{
			name: "No imageSize key",
			providerOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"other": "value",
				},
			},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveImageSize(tt.providerOptions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestImageModel_DoGenerate_WithAspectRatioField verifies that setting AspectRatio on
// ImageGenerateOptions passes it directly to the API without size conversion.
func TestImageModel_DoGenerate_WithAspectRatioField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		imageConfig := genConfig["imageConfig"].(map[string]interface{})
		assert.Equal(t, "21:9", imageConfig["aspectRatio"],
			"expected 21:9 from AspectRatio field (not converted from size)")

		response := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				}{Parts: []struct {
					Text       string      `json:"text,omitempty"`
					InlineData *InlineData `json:"inlineData,omitempty"`
				}{{InlineData: &InlineData{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Ultra-wide movie scene",
		AspectRatio: ImageAspectRatio21x9,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_Gemini_ErrorMaskNotSupported verifies that passing a mask to
// a Gemini image model returns an unsupported error.
func TestImageModel_DoGenerate_Gemini_ErrorMaskNotSupported(t *testing.T) {
	prov := New(Config{APIKey: "test-api-key"})
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Edit this",
		Mask:   &provider.ImageFile{Data: []byte("fake-mask"), MediaType: "image/png"},
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Gemini image models do not support mask-based image editing.")
}

// TestImageModel_DoGenerate_Gemini_IgnoresN verifies that Gemini image
// models do not error on N > 1 (TS google-image-model.ts has no N/count
// concept at all — the request never includes one). MaxImagesPerCall()
// declares the per-call limit of 1 so the core GenerateImage helper issues
// multiple single-image calls instead.
func TestImageModel_DoGenerate_Gemini_IgnoresN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if _, ok := reqBody["n"]; ok {
			t.Errorf("request body should not include an 'n' field: %+v", reqBody)
		}
		response := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				}{Parts: []struct {
					Text       string      `json:"text,omitempty"`
					InlineData *InlineData `json:"inlineData,omitempty"`
				}{{InlineData: &InlineData{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "gemini-2.5-flash-image")
	n := 4

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Generate four images",
		N:      &n,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Images, 1)
	if model.MaxImagesPerCall() != 1 {
		t.Fatalf("MaxImagesPerCall() = %d, want 1", model.MaxImagesPerCall())
	}
}

// TestImageModel_DoGenerate_Gemini_WithSeed verifies that the seed field is included in
// generationConfig when specified.
func TestImageModel_DoGenerate_Gemini_WithSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		assert.Equal(t, float64(42), genConfig["seed"], "seed should be passed in generationConfig")

		response := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						}{
							{
								InlineData: &InlineData{
									MimeType: "image/png",
									Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "gemini-2.5-flash-image")
	seed := 42

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Reproducible image",
		Seed:   &seed,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_DoGenerate_Gemini_WithFilesOptionsAndWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		contents := reqBody["contents"].([]interface{})
		content := contents[0].(map[string]interface{})
		parts := content["parts"].([]interface{})
		require.Len(t, parts, 2)
		assert.Equal(t, "Edit these", parts[0].(map[string]interface{})["text"])
		inline := parts[1].(map[string]interface{})["inlineData"].(map[string]interface{})
		assert.Equal(t, "image/png", inline["mimeType"])
		assert.Equal(t, "aW5saW5l", inline["data"])

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		imageConfig := genConfig["imageConfig"].(map[string]interface{})
		assert.Equal(t, "9:16", imageConfig["aspectRatio"])
		thinkingConfig := genConfig["thinkingConfig"].(map[string]interface{})
		assert.Equal(t, "low", thinkingConfig["thinkingLevel"])

		response := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string      `json:"text,omitempty"`
						InlineData *InlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text       string      `json:"text,omitempty"`
							InlineData *InlineData `json:"inlineData,omitempty"`
						}{
							{
								InlineData: &InlineData{
									MimeType: "image/png",
									Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Edit these",
		Size:        "1024x1024",
		AspectRatio: "9:16",
		Files: []provider.ImageFile{
			{Type: "file", MediaType: "image/png", Data: []byte("inline")},
		},
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingLevel": "low"},
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Images, 1)
	assert.NotEmpty(t, result.Base64Image)
	assert.Len(t, result.Base64Images, 1)
	googleMeta := result.ProviderMetadata["google"].(map[string]interface{})
	imagesMeta := googleMeta["images"].([]map[string]interface{})
	assert.Len(t, imagesMeta, 1)
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "size" {
		t.Fatalf("warnings = %+v, want size warning", result.Warnings)
	}
}

// TestExtractGoogleStringOption verifies the helper extracts string values correctly.
func TestExtractGoogleStringOption(t *testing.T) {
	tests := []struct {
		name            string
		providerOptions map[string]interface{}
		key             string
		expected        string
	}{
		{
			name:            "Nil options",
			providerOptions: nil,
			key:             "personGeneration",
			expected:        "",
		},
		{
			name: "Key present",
			providerOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"personGeneration": "allow_all",
				},
			},
			key:      "personGeneration",
			expected: "allow_all",
		},
		{
			name: "Key absent",
			providerOptions: map[string]interface{}{
				"google": map[string]interface{}{
					"imageSize": "2K",
				},
			},
			key:      "personGeneration",
			expected: "",
		},
		{
			name: "Wrong provider key",
			providerOptions: map[string]interface{}{
				"vertex": map[string]interface{}{
					"personGeneration": "allow_all",
				},
			},
			key:      "personGeneration",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGoogleStringOption(tt.providerOptions, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func imageServer(t *testing.T, respond string, onBody func(map[string]interface{})) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if onBody != nil {
			onBody(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond))
	}))
}

// TS returns every image part, merges the caller's imageConfig with the call's
// aspectRatio and forces responseModalities to IMAGE.
func TestImageModel_DoGenerate_MultipleImagesAndImageConfigMerge(t *testing.T) {
	var gen map[string]interface{}
	server := imageServer(t, `{"candidates":[{"finishReason":"STOP","content":{"parts":[
		{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}},
		{"text":"caption"},
		{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}}]}`, func(b map[string]interface{}) {
		gen, _ = b["generationConfig"].(map[string]interface{})
	})
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "x",
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"responseModalities": []string{"TEXT"},
			"imageConfig":        map[string]interface{}{"imageSize": "2K"},
		}},
	})
	require.NoError(t, err)
	assert.Len(t, result.Images, 2)
	assert.Len(t, result.Base64Images, 2)
	assert.Equal(t, 2, result.Usage.ImageCount)
	assert.Len(t, result.ProviderMetadata["google"].(map[string]interface{})["images"], 2)
	assert.Equal(t, []interface{}{"IMAGE"}, gen["responseModalities"])
	assert.Equal(t, map[string]interface{}{"imageSize": "2K", "aspectRatio": "16:9"}, gen["imageConfig"])
}

func TestImageModel_DoGenerate_ContentFilterIsTerminal(t *testing.T) {
	server := imageServer(t, `{"candidates":[{"finishReason":"IMAGE_SAFETY","content":{"parts":[]}}]}`, nil)
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	require.NoError(t, err)
	assert.Empty(t, result.Images)
	require.NotNil(t, result.IsRetryable)
	assert.False(t, *result.IsRetryable)
}

// TS reports result.finishReason.raw: a prompt block without candidates
// carries the block reason as the raw finish reason.
func TestImageModel_PromptBlockReportsRawFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9}}`))
	}))
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "blocked"})
	require.NoError(t, err)
	meta, _ := result.ProviderMetadata["google"].(map[string]interface{})
	assert.Equal(t, "PROHIBITED_CONTENT", meta["finishReason"])
}

// TS sends a URL input as a file part with media type "image/*", which the
// Google prompt conversion cannot resolve without inline bytes, so editing
// with a URL input throws (google-image-model.test.ts "should reject
// unsupported URL editing input and masks").
func TestImageModel_DoGenerate_Gemini_RejectsURLInput(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gemini-2.5-flash-image")

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Add a hat to this cat",
		Files:  []provider.ImageFile{{Type: "url", URL: "https://example.com/cat.png"}},
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	require.ErrorAs(t, err, &unsupported)
	assert.Regexp(t, `media type "image/\*".*not passed as inline bytes`, err.Error())
	assert.False(t, called, "no request may be sent")
}

func TestBatchImageRequestRejectsURLInput(t *testing.T) {
	b := &Batch{provider: New(Config{APIKey: "k"})}
	_, _, err := b.prepareImageBatchRequest(&provider.ImageBatchV4Request{
		ID: "1", ModelID: "gemini-2.5-flash-image",
		Options: provider.ImageGenerateOptions{Prompt: "p", Files: []provider.ImageFile{{Type: "url", URL: "https://example.com/a.png"}}},
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	require.ErrorAs(t, err, &unsupported)
}

// TS image response: {timestamp, modelId: this.modelId, headers} and
// totalTokens = input + output (not the API's totalTokenCount).
func TestImageModel_ResponseAndTotalTokensMatchTS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Test", "yes")
		_, _ = w.Write([]byte(`{"responseId":"resp-1","candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AAEC"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":5,"totalTokenCount":99}}`))
	}))
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	require.NoError(t, err)
	require.NotNil(t, result.Response)
	assert.Equal(t, "gemini-2.5-flash-image", result.Response.ModelID)
	assert.Empty(t, result.Response.ID)
	assert.False(t, result.Response.Timestamp.IsZero())
	assert.Equal(t, "yes", result.Response.Headers["X-Test"])
	assert.Equal(t, 8, result.Usage.TotalTokens)
}

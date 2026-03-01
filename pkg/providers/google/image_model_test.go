package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageModel_SpecificationVersion(t *testing.T) {
	model := NewImageModel(nil, "imagen-4.0-generate-001")
	assert.Equal(t, "v3", model.SpecificationVersion())
}

func TestImageModel_Provider(t *testing.T) {
	model := NewImageModel(nil, "imagen-4.0-generate-001")
	assert.Equal(t, "google", model.Provider())
}

func TestImageModel_ModelID(t *testing.T) {
	modelID := "imagen-4.0-generate-001"
	model := NewImageModel(nil, modelID)
	assert.Equal(t, modelID, model.ModelID())
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

func TestImageModel_DoGenerate_Imagen(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "imagen-4.0-generate-001:predict")
		assert.Contains(t, r.URL.RawQuery, "key=test-api-key")

		// Parse request body
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request structure
		instances, ok := reqBody["instances"].([]interface{})
		require.True(t, ok)
		require.Len(t, instances, 1)

		instance := instances[0].(map[string]interface{})
		assert.Equal(t, "A beautiful sunset", instance["prompt"])

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, float64(1), params["sampleCount"])
		assert.Equal(t, "1:1", params["aspectRatio"])

		// Return mock response
		response := imagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
				Prompt             string `json:"prompt,omitempty"`
			}{
				{
					BytesBase64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", // 1x1 red pixel
					MimeType:           "image/png",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with mock server
	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	// Create model
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	// Generate image
	n := 1
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A beautiful sunset",
		N:      &n,
		Size:   "1024x1024",
	})

	// Verify result
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Image)
	assert.Equal(t, "image/png", result.MimeType)
	assert.Equal(t, 1, result.Usage.ImageCount)
}

func TestImageModel_DoGenerate_Gemini(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "gemini-2.5-flash-image:generateContent")
		assert.Contains(t, r.URL.RawQuery, "key=test-api-key")

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
		json.NewEncoder(w).Encode(response)
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
	assert.Equal(t, "image/jpeg", result.MimeType)
	assert.Equal(t, 1, result.Usage.ImageCount)
}

func TestImageModel_DoGenerate_Error_EmptyResponse(t *testing.T) {
	// Create mock server that returns empty response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := imagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
				Prompt             string `json:"prompt,omitempty"`
			}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model := NewImageModel(prov, "imagen-4.0-generate-001")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Test",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no images in response")
}

func TestImageModel_DoGenerate_Error_InvalidStatus(t *testing.T) {
	// Create mock server that returns error status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid request"}`))
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
	})

	model := NewImageModel(prov, "imagen-4.0-generate-001")
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
		assert.Contains(t, r.URL.RawQuery, "key=test-api-key")

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
		json.NewEncoder(w).Encode(response)
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
		json.NewDecoder(r.Body).Decode(&reqBody)

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "21:9", params["aspectRatio"],
			"expected 21:9 from AspectRatio field (not converted from size)")

		response := imagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
				Prompt             string `json:"prompt,omitempty"`
			}{
				{
					BytesBase64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
					MimeType:           "image/png",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Ultra-wide movie scene",
		AspectRatio: ImageAspectRatio21x9,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_Imagen_ErrorFilesNotSupported verifies that passing files to
// an Imagen model returns an unsupported error (matches TS behavior).
func TestImageModel_DoGenerate_Imagen_ErrorFilesNotSupported(t *testing.T) {
	prov := New(Config{APIKey: "test-api-key"})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Edit this image",
		Files:  []provider.ImageFile{{Data: []byte("fake-image"), MediaType: "image/png"}},
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "image editing with files is not supported")
	assert.Contains(t, err.Error(), "Vertex AI")
}

// TestImageModel_DoGenerate_Imagen_ErrorMaskNotSupported verifies that passing a mask to
// an Imagen model returns an unsupported error (matches TS behavior).
func TestImageModel_DoGenerate_Imagen_ErrorMaskNotSupported(t *testing.T) {
	prov := New(Config{APIKey: "test-api-key"})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Inpaint this",
		Mask:   &provider.ImageFile{Data: []byte("fake-mask"), MediaType: "image/png"},
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "image editing with masks is not supported")
	assert.Contains(t, err.Error(), "Vertex AI")
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
	assert.Contains(t, err.Error(), "image editing with masks is not supported")
}

// TestImageModel_DoGenerate_Gemini_ErrorMultipleImages verifies that requesting N > 1 from
// a Gemini image model returns an unsupported error.
func TestImageModel_DoGenerate_Gemini_ErrorMultipleImages(t *testing.T) {
	prov := New(Config{APIKey: "test-api-key"})
	model := NewImageModel(prov, "gemini-2.5-flash-image")
	n := 4

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Generate four images",
		N:      &n,
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "do not support generating multiple images")
}

// TestImageModel_DoGenerate_Imagen_DefaultAspectRatio verifies that when no aspectRatio
// or size is provided, the Imagen model defaults to 1:1 (matches TS behavior).
func TestImageModel_DoGenerate_Imagen_DefaultAspectRatio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "1:1", params["aspectRatio"], "should default to 1:1 when no aspectRatio/size provided")

		response := imagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
				Prompt             string `json:"prompt,omitempty"`
			}{
				{
					BytesBase64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
					MimeType:           "image/png",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A simple image",
		// No AspectRatio, no Size — should default to 1:1
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_Imagen_PersonGeneration verifies that the personGeneration
// provider option is passed through to the Imagen API parameters.
func TestImageModel_DoGenerate_Imagen_PersonGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "allow_adult", params["personGeneration"])

		response := imagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
				Prompt             string `json:"prompt,omitempty"`
			}{
				{
					BytesBase64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
					MimeType:           "image/png",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A photo of a person",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"personGeneration": "allow_adult",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_Gemini_WithSeed verifies that the seed field is included in
// generationConfig when specified.
func TestImageModel_DoGenerate_Gemini_WithSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

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
		json.NewEncoder(w).Encode(response)
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


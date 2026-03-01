package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageModel_SpecificationVersion(t *testing.T) {
	model := NewImageModel(nil, "imagen-3.0-generate-001")
	assert.Equal(t, "v3", model.SpecificationVersion())
}

func TestImageModel_Provider(t *testing.T) {
	model := NewImageModel(nil, "imagen-3.0-generate-001")
	assert.Equal(t, "google-vertex", model.Provider())
}

func TestImageModel_ModelID(t *testing.T) {
	modelID := "imagen-3.0-generate-001"
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
			name:     "Imagen model",
			modelID:  "imagen-3.0-generate-001",
			expected: false,
		},
		{
			name:     "Imagen fast model",
			modelID:  "imagen-3.0-fast-generate-001",
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
			name:     "Landscape 16:9",
			size:     "1920x1080",
			expected: "16:9",
		},
		{
			name:     "Portrait 9:16",
			size:     "1080x1920",
			expected: "9:16",
		},
		{
			name:     "Unknown size",
			size:     "999x999",
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
		assert.Contains(t, r.URL.Path, "imagen-3.0-generate-001:predict")
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Parse request body
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request structure
		instances, ok := reqBody["instances"].([]interface{})
		require.True(t, ok)
		require.Len(t, instances, 1)

		instance := instances[0].(map[string]interface{})
		assert.Equal(t, "A mountain landscape", instance["prompt"])

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, float64(1), params["sampleCount"])

		// Return mock response
		response := vertexImagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
				Prompt             string `json:"prompt,omitempty"`
			}{
				{
					BytesBase64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
					MimeType:           "image/png",
					Prompt:             "A mountain landscape",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with mock server
	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)

	// Create model
	model := NewImageModel(prov, "imagen-3.0-generate-001")

	// Generate image
	n := 1
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A mountain landscape",
		N:      &n,
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

		// Parse request body
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request structure
		contents, ok := reqBody["contents"].([]interface{})
		require.True(t, ok)
		require.Len(t, contents, 1)

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		modalities := genConfig["responseModalities"].([]interface{})
		assert.Contains(t, modalities, "IMAGE")

		// Return mock response
		response := vertexGeminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string                     `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text       string                     `json:"text,omitempty"`
							InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text       string                     `json:"text,omitempty"`
							InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
						}{
							{
								InlineData: &vertexGeminiInlineData{
									MimeType: "image/png",
									Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
								},
							},
						},
					},
				},
			},
			UsageMetadata: &struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			}{
				PromptTokenCount:     10,
				CandidatesTokenCount: 20,
				TotalTokenCount:      30,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with mock server
	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)

	// Create model
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	// Generate image
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Colorful abstract",
	})

	// Verify result
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Image)
	assert.Equal(t, "image/png", result.MimeType)
	assert.Equal(t, 1, result.Usage.ImageCount)
}

func TestImageModel_DoGenerate_WithAspectRatio(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// Verify aspect ratio is set
		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "16:9", params["aspectRatio"])

		response := vertexImagenResponse{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "imagen-3.0-generate-001")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Wide landscape",
		Size:   "1920x1080", // Should convert to 16:9
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_DoGenerate_Error_EmptyPredictions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := vertexImagenResponse{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "imagen-3.0-generate-001")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Test",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no images in response")
}

func TestImageModel_DoGenerate_Error_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"code": 401, "message": "Unauthorized"}}`))
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "invalid-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "imagen-3.0-generate-001")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Test",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "401")
}

func TestGetIntValue(t *testing.T) {
	tests := []struct {
		name     string
		ptr      *int
		defVal   int
		expected int
	}{
		{
			name:     "Nil pointer returns default",
			ptr:      nil,
			defVal:   3,
			expected: 3,
		},
		{
			name:     "Non-nil pointer returns value",
			ptr:      intPtr(7),
			defVal:   3,
			expected: 7,
		},
		{
			name:     "Zero value",
			ptr:      intPtr(0),
			defVal:   3,
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

func TestProvider_New_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError string
	}{
		{
			name: "Missing project",
			config: Config{
				Location:    "us-central1",
				AccessToken: "token",
			},
			expectError: "project is required",
		},
		{
			name: "Missing location",
			config: Config{
				Project:     "my-project",
				AccessToken: "token",
			},
			expectError: "location is required",
		},
		{
			name: "Missing access token",
			config: Config{
				Project:  "my-project",
				Location: "us-central1",
			},
			expectError: "access token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, err := New(tt.config)
			assert.Error(t, err)
			assert.Nil(t, prov)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestProvider_New_Success(t *testing.T) {
	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	})

	require.NoError(t, err)
	require.NotNil(t, prov)
	assert.Equal(t, "google-vertex", prov.Name())
	assert.Equal(t, "test-project", prov.Project())
	assert.Equal(t, "us-central1", prov.Location())
}

func TestProvider_ImageModel_EmptyModelID(t *testing.T) {
	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	})

	model, err := prov.ImageModel("")
	assert.Error(t, err)
	assert.Nil(t, model)
	assert.Contains(t, err.Error(), "model ID cannot be empty")
}

func TestResolveAspectRatio(t *testing.T) {
	tests := []struct {
		name        string
		aspectRatio string
		size        string
		expected    string
	}{
		{
			name:        "AspectRatio takes precedence",
			aspectRatio: "16:9",
			size:        "1024x1024",
			expected:    "16:9",
		},
		{
			name:        "AspectRatio used directly",
			aspectRatio: "3:4",
			size:        "",
			expected:    "3:4",
		},
		{
			name:        "Size converted when no AspectRatio",
			aspectRatio: "",
			size:        "1920x1080",
			expected:    "16:9",
		},
		{
			name:        "Both empty",
			aspectRatio: "",
			size:        "",
			expected:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveAspectRatio(tt.aspectRatio, tt.size)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVertexImageSizeConstants(t *testing.T) {
	assert.Equal(t, "1K", VertexImageSize1K)
	assert.Equal(t, "2K", VertexImageSize2K)
}

func TestResolveVertexImageSize(t *testing.T) {
	tests := []struct {
		name            string
		providerOptions map[string]interface{}
		expected        string
	}{
		{
			name:            "Nil options",
			providerOptions: nil,
			expected:        "",
		},
		{
			name: "sampleImageSize 1K",
			providerOptions: map[string]interface{}{
				"vertex": map[string]interface{}{
					"sampleImageSize": "1K",
				},
			},
			expected: "1K",
		},
		{
			name: "sampleImageSize 2K",
			providerOptions: map[string]interface{}{
				"vertex": map[string]interface{}{
					"sampleImageSize": "2K",
				},
			},
			expected: "2K",
		},
		{
			name: "No vertex key",
			providerOptions: map[string]interface{}{
				"google": map[string]interface{}{},
			},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveVertexImageSize(tt.providerOptions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestImageModel_DoGenerate_WithAspectRatioField verifies AspectRatio field is used directly.
func TestImageModel_DoGenerate_WithAspectRatioField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "9:16", params["aspectRatio"])

		response := vertexImagenResponse{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "imagen-3.0-generate-001")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Portrait photo",
		AspectRatio: "9:16",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_WithSampleImageSize verifies sampleImageSize is passed correctly.
func TestImageModel_DoGenerate_WithSampleImageSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "2K", params["sampleImageSize"])

		response := vertexImagenResponse{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "imagen-4.0-generate-001")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "High resolution photo",
		ProviderOptions: map[string]interface{}{
			"vertex": map[string]interface{}{
				"sampleImageSize": VertexImageSize2K,
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestVertexModelConstants verifies new model IDs from #12819 and #12883 are present.
func TestVertexModelConstants(t *testing.T) {
	assert.Equal(t, "gemini-3.1-pro-preview", ModelGemini31ProPreview)
	assert.Equal(t, "gemini-3.1-flash-image-preview", ModelGemini31FlashImagePreview)
	assert.Equal(t, "gemini-3-pro-preview", ModelGemini3ProPreview)
	assert.Equal(t, "gemini-3-pro-image-preview", ModelGemini3ProImagePreview)
	assert.Equal(t, "gemini-3-flash-preview", ModelGemini3FlashPreview)
	assert.Equal(t, "imagen-4.0-generate-001", ModelImagen40Generate001)
	assert.Equal(t, "imagen-4.0-ultra-generate-001", ModelImagen40UltraGenerate001)
	assert.Equal(t, "imagen-4.0-fast-generate-001", ModelImagen40FastGenerate001)
}

// TestImageModel_DoGenerate_Gemini_ErrorMaskNotSupported verifies that passing a mask to
// a Gemini image model returns an unsupported error (matches TS behavior).
func TestImageModel_DoGenerate_Gemini_ErrorMaskNotSupported(t *testing.T) {
	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	})
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
// a Gemini image model returns an unsupported error (matches TS behavior).
func TestImageModel_DoGenerate_Gemini_ErrorMultipleImages(t *testing.T) {
	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	})
	model := NewImageModel(prov, "gemini-2.5-flash-image")
	n := 3

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Three images please",
		N:      &n,
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "do not support generating multiple images")
}

// TestImageModel_DoGenerate_Imagen_WithSeed verifies that the seed is included in
// Imagen parameters when specified.
func TestImageModel_DoGenerate_Imagen_WithSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, float64(99), params["seed"], "seed should be in parameters")

		response := vertexImagenResponse{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	model := NewImageModel(prov, "imagen-3.0-generate-001")
	seed := 99

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Reproducible landscape",
		Seed:   &seed,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_Imagen_FullProviderOptions verifies that all Vertex provider
// options (negativePrompt, personGeneration, safetySetting, addWatermark, storageUri)
// are passed through to the API.
func TestImageModel_DoGenerate_Imagen_FullProviderOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		// negativePrompt is in the instance
		instances := reqBody["instances"].([]interface{})
		instance := instances[0].(map[string]interface{})
		assert.Equal(t, "blurry, distorted", instance["negativePrompt"])

		// Others are in parameters
		params := reqBody["parameters"].(map[string]interface{})
		assert.Equal(t, "allow_adult", params["personGeneration"])
		assert.Equal(t, "block_some", params["safetySetting"])
		assert.Equal(t, true, params["addWatermark"])
		assert.Equal(t, "gs://my-bucket/output", params["storageUri"])

		response := vertexImagenResponse{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	model := NewImageModel(prov, "imagen-4.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A beautiful portrait",
		ProviderOptions: map[string]interface{}{
			"vertex": map[string]interface{}{
				"negativePrompt":   "blurry, distorted",
				"personGeneration": "allow_adult",
				"safetySetting":    "block_some",
				"addWatermark":     true,
				"storageUri":       "gs://my-bucket/output",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_Gemini_WithSeed verifies that seed is passed in generationConfig
// for Gemini image models.
func TestImageModel_DoGenerate_Gemini_WithSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		assert.Equal(t, float64(7), genConfig["seed"], "seed should be in generationConfig")

		response := vertexGeminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text       string                  `json:"text,omitempty"`
							InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text       string                  `json:"text,omitempty"`
							InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
						}{
							{
								InlineData: &vertexGeminiInlineData{
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

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	model := NewImageModel(prov, "gemini-2.5-flash-image")
	seed := 7

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Deterministic image",
		Seed:   &seed,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestExtractVertexOptions verifies the helper returns the vertex options map.
func TestExtractVertexOptions(t *testing.T) {
	tests := []struct {
		name     string
		opts     map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "Nil options",
			opts:     nil,
			expected: map[string]interface{}{},
		},
		{
			name: "Vertex options present",
			opts: map[string]interface{}{
				"vertex": map[string]interface{}{
					"negativePrompt": "blurry",
				},
			},
			expected: map[string]interface{}{"negativePrompt": "blurry"},
		},
		{
			name: "No vertex key",
			opts: map[string]interface{}{
				"google": map[string]interface{}{},
			},
			expected: map[string]interface{}{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVertexOptions(tt.opts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}

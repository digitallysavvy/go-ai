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
	model := NewImageModel(nil, "gemini-2.5-flash-image")
	assert.Equal(t, "v4", model.SpecificationVersion())
}

func TestImageModel_Provider(t *testing.T) {
	model := NewImageModel(nil, "gemini-2.5-flash-image")
	assert.Equal(t, "google-vertex", model.Provider())
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
	prov, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "test-token"})
	require.NoError(t, err)
	model := NewImageModel(prov, "imagen-3.0-generate-001")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A mountain landscape",
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
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
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
	assert.Len(t, result.Images, 1)
	assert.NotEmpty(t, result.Base64Image)
	assert.Len(t, result.Base64Images, 1)
	assert.Equal(t, "image/png", result.MimeType)
	assert.Equal(t, 1, result.Usage.ImageCount)
	vertexMeta := result.ProviderMetadata["googleVertex"].(map[string]interface{})
	vertexImages := vertexMeta["images"].([]map[string]interface{})
	assert.Len(t, vertexImages, 1)
	legacyMeta := result.ProviderMetadata["vertex"].(map[string]interface{})
	legacyImages := legacyMeta["images"].([]map[string]interface{})
	assert.Len(t, legacyImages, 1)
}

func TestImageModel_DoGenerate_Gemini_WithFilesOptionsAndWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		contents := reqBody["contents"].([]interface{})
		content := contents[0].(map[string]interface{})
		parts := content["parts"].([]interface{})
		require.Len(t, parts, 3)
		assert.Equal(t, "Edit these", parts[0].(map[string]interface{})["text"])
		inline := parts[1].(map[string]interface{})["inlineData"].(map[string]interface{})
		assert.Equal(t, "image/png", inline["mimeType"])
		assert.Equal(t, "aW5saW5l", inline["data"])
		fileData := parts[2].(map[string]interface{})["fileData"].(map[string]interface{})
		assert.Equal(t, "https://example.com/ref.png", fileData["fileUri"])
		assert.Equal(t, "image/*", fileData["mimeType"])

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		imageConfig := genConfig["imageConfig"].(map[string]interface{})
		assert.Equal(t, "9:16", imageConfig["aspectRatio"])
		assert.Equal(t, "low", genConfig["thinkingBudget"])

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
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	model := NewImageModel(prov, "gemini-2.5-flash-image")

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Edit these",
		Size:        "1024x1024",
		AspectRatio: "9:16",
		Files: []provider.ImageFile{
			{Type: "file", MediaType: "image/png", Data: []byte("inline")},
			{Type: "url", URL: "https://example.com/ref.png"},
		},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"thinkingBudget": "low",
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Images, 1)
	assert.NotEmpty(t, result.Base64Image)
	assert.Len(t, result.Base64Images, 1)
	vertexMeta := result.ProviderMetadata["googleVertex"].(map[string]interface{})
	vertexImages := vertexMeta["images"].([]map[string]interface{})
	assert.Len(t, vertexImages, 1)
	legacyMeta := result.ProviderMetadata["vertex"].(map[string]interface{})
	legacyImages := legacyMeta["images"].([]map[string]interface{})
	assert.Len(t, legacyImages, 1)
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "size" {
		t.Fatalf("warnings = %+v, want size warning", result.Warnings)
	}
}

// TestImageModel_DoGenerate_WithAspectRatio verifies that AspectRatio is
// forwarded into imageConfig for Gemini image models.
func TestImageModel_DoGenerate_WithAspectRatio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody) //nolint:errcheck

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		imageConfig := genConfig["imageConfig"].(map[string]interface{})
		assert.Equal(t, "16:9", imageConfig["aspectRatio"])

		response := vertexGeminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				}{Parts: []struct {
					Text       string                  `json:"text,omitempty"`
					InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
				}{{InlineData: &vertexGeminiInlineData{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}}}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Wide landscape",
		AspectRatio: "16:9",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_DoGenerate_Error_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := vertexGeminiImageResponse{}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Test",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no image in response")
}

func TestImageModel_DoGenerate_Error_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"code": 401, "message": "Unauthorized"}}`))
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "invalid-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
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
		_ = json.NewDecoder(r.Body).Decode(&reqBody) //nolint:errcheck

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		imageConfig := genConfig["imageConfig"].(map[string]interface{})
		assert.Equal(t, "9:16", imageConfig["aspectRatio"])

		response := vertexGeminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				}{Parts: []struct {
					Text       string                  `json:"text,omitempty"`
					InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
				}{{InlineData: &vertexGeminiInlineData{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "Portrait photo",
		AspectRatio: "9:16",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestImageModel_DoGenerate_WithSampleImageSize verifies sampleImageSize is passed correctly
// via imageConfig.imageSize for Gemini image models.
func TestImageModel_DoGenerate_WithSampleImageSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody) //nolint:errcheck

		genConfig := reqBody["generationConfig"].(map[string]interface{})
		imageConfig := genConfig["imageConfig"].(map[string]interface{})
		assert.Equal(t, "2K", imageConfig["imageSize"])

		response := vertexGeminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				}{Parts: []struct {
					Text       string                  `json:"text,omitempty"`
					InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
				}{{InlineData: &vertexGeminiInlineData{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
	}))
	defer server.Close()

	prov, _ := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})

	model := NewImageModel(prov, "gemini-2.5-flash-image")
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
	assert.Equal(t, "gemini-2.5-flash-tts", SpeechModelGemini25FlashTTS)
	assert.Equal(t, "gemini-2.5-pro-tts", SpeechModelGemini25ProTTS)
	assert.Equal(t, "gemini-2.5-flash-lite-preview-tts", SpeechModelGemini25FlashLitePreviewTTS)
	assert.Equal(t, "gemini-3.1-flash-tts-preview", SpeechModelGemini31FlashTTSPreview)
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
	assert.Contains(t, err.Error(), "Gemini image models do not support mask-based image editing.")
}

// TestImageModel_DoGenerate_Gemini_IgnoresN verifies that Gemini image
// models do not error on N > 1: TS google-image-model.ts has no N/count
// concept, and MaxImagesPerCall() declares the per-call limit of 1 so the
// core GenerateImage helper issues multiple single-image calls instead.
func TestImageModel_DoGenerate_Gemini_IgnoresN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if _, ok := reqBody["n"]; ok {
			t.Errorf("request body should not include an 'n' field: %+v", reqBody)
		}
		response := vertexGeminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []struct {
						Text       string                  `json:"text,omitempty"`
						InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
					} `json:"parts"`
				}{Parts: []struct {
					Text       string                  `json:"text,omitempty"`
					InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
				}{{InlineData: &vertexGeminiInlineData{MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	require.NoError(t, err)
	model := NewImageModel(prov, "gemini-2.5-flash-image")
	n := 3

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "Three images please",
		N:      &n,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Images, 1)
	if model.MaxImagesPerCall() != 1 {
		t.Fatalf("MaxImagesPerCall() = %d, want 1", model.MaxImagesPerCall())
	}
}

// TestImageModel_DoGenerate_Gemini_WithSeed verifies that seed is passed in generationConfig
// for Gemini image models.
func TestImageModel_DoGenerate_Gemini_WithSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody) //nolint:errcheck

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
		_ = json.NewEncoder(w).Encode(response) //nolint:errcheck
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

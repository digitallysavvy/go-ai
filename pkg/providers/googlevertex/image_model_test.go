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

// Helper function
func intPtr(i int) *int {
	return &i
}

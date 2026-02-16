package google

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

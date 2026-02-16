package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements image generation for Google Generative AI
// Supports both Imagen models (via :predict API) and Gemini image models (via :generateContent API)
type ImageModel struct {
	prov    *Provider
	modelID string
}

// NewImageModel creates a new Google Generative AI image generation model
func NewImageModel(prov *Provider, modelID string) *ImageModel {
	return &ImageModel{
		prov:    prov,
		modelID: modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "google"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Determine if this is a Gemini model or Imagen model
	if isGeminiModel(m.modelID) {
		return m.doGenerateGemini(ctx, opts)
	}
	return m.doGenerateImagen(ctx, opts)
}

// doGenerateImagen generates images using the Imagen API (:predict endpoint)
func (m *ImageModel) doGenerateImagen(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Build request body for Imagen
	reqBody := map[string]interface{}{
		"instances": []map[string]interface{}{
			{
				"prompt": opts.Prompt,
			},
		},
		"parameters": map[string]interface{}{
			"sampleCount": getIntValue(opts.N, 1),
		},
	}

	// Add aspect ratio if specified (Imagen uses aspectRatio, not size)
	if opts.Size != "" {
		// Convert size to aspect ratio (e.g., "1024x1024" -> "1:1")
		aspectRatio := convertSizeToAspectRatio(opts.Size)
		if aspectRatio != "" {
			reqBody["parameters"].(map[string]interface{})["aspectRatio"] = aspectRatio
		}
	}

	// Build URL with API key
	path := fmt.Sprintf("/v1beta/models/%s:predict?key=%s", m.modelID, m.prov.APIKey())

	// Make request
	resp, err := m.prov.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("google", 0, "", "failed to generate image: "+err.Error(), err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providererrors.NewProviderError("google", resp.StatusCode, "",
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(resp.Body)), nil)
	}

	// Parse response
	var imagenResp imagenResponse
	if err := json.Unmarshal(resp.Body, &imagenResp); err != nil {
		return nil, providererrors.NewProviderError("google", 0, "", "failed to parse response: "+err.Error(), err)
	}

	if len(imagenResp.Predictions) == 0 {
		return nil, providererrors.NewProviderError("google", 0, "", "no images in response", nil)
	}

	// Decode first image (Google Generative AI returns base64)
	imageData, err := base64.StdEncoding.DecodeString(imagenResp.Predictions[0].BytesBase64Encoded)
	if err != nil {
		return nil, providererrors.NewProviderError("google", 0, "", "failed to decode image: "+err.Error(), err)
	}

	return &types.ImageResult{
		Image:    imageData,
		MimeType: "image/png",
		Usage: types.ImageUsage{
			ImageCount: len(imagenResp.Predictions),
		},
	}, nil
}

// doGenerateGemini generates images using Gemini models via the generateContent API
func (m *ImageModel) doGenerateGemini(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Gemini image models use the language model API with responseModalities: ["IMAGE"]
	// Build request body for Gemini
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"text": opts.Prompt,
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"IMAGE"},
		},
	}

	// Add aspect ratio if specified
	if opts.Size != "" {
		aspectRatio := convertSizeToAspectRatio(opts.Size)
		if aspectRatio != "" {
			genConfig := reqBody["generationConfig"].(map[string]interface{})
			genConfig["imageConfig"] = map[string]interface{}{
				"aspectRatio": aspectRatio,
			}
		}
	}

	// Build URL with API key
	path := fmt.Sprintf("/v1beta/models/%s:generateContent?key=%s", m.modelID, m.prov.APIKey())

	// Make request
	resp, err := m.prov.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("google", 0, "", "failed to generate image: "+err.Error(), err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providererrors.NewProviderError("google", resp.StatusCode, "",
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(resp.Body)), nil)
	}

	// Parse response
	var geminiResp geminiImageResponse
	if err := json.Unmarshal(resp.Body, &geminiResp); err != nil {
		return nil, providererrors.NewProviderError("google", 0, "", "failed to parse response: "+err.Error(), err)
	}

	// Extract image from response
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, providererrors.NewProviderError("google", 0, "", "no image in response", nil)
	}

	// Find the image part (inlineData with image/* mimeType)
	var imageData []byte
	var mimeType string
	for _, part := range geminiResp.Candidates[0].Content.Parts {
		if part.InlineData != nil && len(part.InlineData.MimeType) >= 6 && part.InlineData.MimeType[:6] == "image/" {
			data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return nil, providererrors.NewProviderError("google", 0, "", "failed to decode image: "+err.Error(), err)
			}
			imageData = data
			mimeType = part.InlineData.MimeType
			break
		}
	}

	if imageData == nil {
		return nil, providererrors.NewProviderError("google", 0, "", "no image data in response", nil)
	}

	return &types.ImageResult{
		Image:    imageData,
		MimeType: mimeType,
		Usage: types.ImageUsage{
			ImageCount: 1,
		},
	}, nil
}

// isGeminiModel checks if the model ID is a Gemini image model
func isGeminiModel(modelID string) bool {
	// Gemini image models start with "gemini-"
	return len(modelID) >= 7 && modelID[:7] == "gemini-"
}

// convertSizeToAspectRatio converts size format (e.g., "1024x1024") to aspect ratio (e.g., "1:1")
func convertSizeToAspectRatio(size string) string {
	switch size {
	case "1024x1024", "512x512", "256x256":
		return "1:1"
	case "1024x768":
		return "4:3"
	case "768x1024":
		return "3:4"
	case "1920x1080", "1792x1024":
		return "16:9"
	case "1080x1920", "1024x1792":
		return "9:16"
	default:
		// Return empty for unsupported sizes, will use default
		return ""
	}
}

// getIntValue safely gets int value or default
func getIntValue(ptr *int, defaultVal int) int {
	if ptr != nil {
		return *ptr
	}
	return defaultVal
}

// Response types for Imagen API
type imagenResponse struct {
	Predictions []struct {
		BytesBase64Encoded string `json:"bytesBase64Encoded"`
		MimeType           string `json:"mimeType"`
		Prompt             string `json:"prompt,omitempty"`
	} `json:"predictions"`
}

// Response types for Gemini image API
type geminiImageResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string      `json:"text,omitempty"`
				InlineData *InlineData `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64-encoded
}

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

// ImageAspectRatio constants for Google Generative AI image generation.
//
// For Imagen models, only the five standard ratios are supported:
// 1:1, 3:4, 4:3, 9:16, 16:9.
//
// For Gemini image models (gemini-*), a wider set of aspect ratios is accepted
// via the imageConfig parameter. All constants below are valid for Gemini models.
// Added in #12897.
const (
	// Standard aspect ratios supported by both Imagen and Gemini image models
	ImageAspectRatio1x1  = "1:1"
	ImageAspectRatio3x4  = "3:4"
	ImageAspectRatio4x3  = "4:3"
	ImageAspectRatio9x16 = "9:16"
	ImageAspectRatio16x9 = "16:9"

	// Extended aspect ratios — Gemini image models only (#12897)
	ImageAspectRatio2x3  = "2:3"
	ImageAspectRatio3x2  = "3:2"
	ImageAspectRatio4x5  = "4:5"
	ImageAspectRatio5x4  = "5:4"
	ImageAspectRatio21x9 = "21:9"
	ImageAspectRatio1x8  = "1:8"
	ImageAspectRatio8x1  = "8:1"
	ImageAspectRatio1x4  = "1:4"
	ImageAspectRatio4x1  = "4:1"
)

// ImageSize constants for the imageSize parameter in Gemini image generation.
// Controls the output resolution. Added in #12897.
const (
	ImageSize512 = "512"
	ImageSize1K  = "1K"
	ImageSize2K  = "2K"
	ImageSize4K  = "4K"
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
	// Image editing is not supported for Google Generative AI Imagen models.
	if len(opts.Files) > 0 {
		return nil, fmt.Errorf("image editing with files is not supported for Google Generative AI Imagen models. Use Google Vertex AI instead")
	}
	if opts.Mask != nil {
		return nil, fmt.Errorf("image editing with masks is not supported for Google Generative AI Imagen models. Use Google Vertex AI instead")
	}

	// Resolve aspect ratio: prefer opts.AspectRatio, fall back to size conversion, then default to 1:1.
	aspectRatio := resolveAspectRatio(opts.AspectRatio, opts.Size)
	if aspectRatio == "" {
		aspectRatio = ImageAspectRatio1x1
	}

	parameters := map[string]interface{}{
		"sampleCount": getIntValue(opts.N, 1),
		"aspectRatio": aspectRatio,
	}

	// personGeneration provider option (e.g., "dont_allow", "allow_adult", "allow_all")
	if pgen := extractGoogleStringOption(opts.ProviderOptions, "personGeneration"); pgen != "" {
		parameters["personGeneration"] = pgen
	}

	// Build request body for Imagen
	reqBody := map[string]interface{}{
		"instances": []map[string]interface{}{
			{
				"prompt": opts.Prompt,
			},
		},
		"parameters": parameters,
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
	// Image editing with masks is not supported for Gemini image models.
	if opts.Mask != nil {
		return nil, fmt.Errorf("image editing with masks is not supported for Gemini image models")
	}
	// Gemini image models only support generating a single image at a time.
	if opts.N != nil && *opts.N > 1 {
		return nil, fmt.Errorf("Gemini image models do not support generating multiple images. Use Imagen models for multiple image generation")
	}

	// Gemini image models use the language model API with responseModalities: ["IMAGE"]
	genConfig := map[string]interface{}{
		"responseModalities": []string{"IMAGE"},
	}

	// Add seed if specified for reproducible generation.
	if opts.Seed != nil {
		genConfig["seed"] = *opts.Seed
	}

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
		"generationConfig": genConfig,
	}

	// Add aspect ratio and/or imageSize if specified.
	// Prefer opts.AspectRatio directly; fall back to converting from opts.Size.
	imageConfig := map[string]interface{}{}
	if aspectRatio := resolveAspectRatio(opts.AspectRatio, opts.Size); aspectRatio != "" {
		imageConfig["aspectRatio"] = aspectRatio
	}
	if imageSize := extractGoogleStringOption(opts.ProviderOptions, "imageSize"); imageSize != "" {
		imageConfig["imageSize"] = imageSize
	}
	if len(imageConfig) > 0 {
		genConfig["imageConfig"] = imageConfig
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

// resolveAspectRatio returns the aspect ratio to send to the API.
// If aspectRatio is already set (e.g., "16:9"), it is used directly.
// Otherwise, size (e.g., "1920x1080") is converted to an aspect ratio.
func resolveAspectRatio(aspectRatio, size string) string {
	if aspectRatio != "" {
		return aspectRatio
	}
	return convertSizeToAspectRatio(size)
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
		return ""
	}
}

// extractGoogleStringOption extracts a string value from ProviderOptions["google"][key].
func extractGoogleStringOption(providerOptions map[string]interface{}, key string) string {
	if providerOptions == nil {
		return ""
	}
	googleOpts, ok := providerOptions["google"].(map[string]interface{})
	if !ok {
		return ""
	}
	val, _ := googleOpts[key].(string)
	return val
}

// resolveImageSize extracts the imageSize option from provider options for Gemini image models.
// Provider options format: map["google"]map["imageSize"] = "1K"
func resolveImageSize(providerOptions map[string]interface{}) string {
	return extractGoogleStringOption(providerOptions, "imageSize")
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

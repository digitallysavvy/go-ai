package googlevertex

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

// ImageModel implements image generation for Google Vertex AI
// Supports both Imagen models (via :predict API) and Gemini image models (via :generateContent API)
type ImageModel struct {
	prov    *Provider
	modelID string
}

// NewImageModel creates a new Google Vertex AI image generation model
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
	return "google-vertex"
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
// Supports both text-to-image and image editing (inpainting, outpainting, etc.)
func (m *ImageModel) doGenerateImagen(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Build instance — negativePrompt is an instance-level field (not a parameter).
	instance := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	vertexOpts := extractVertexOptions(opts.ProviderOptions)

	if negPrompt, ok := vertexOpts["negativePrompt"].(string); ok && negPrompt != "" {
		instance["negativePrompt"] = negPrompt
	}

	parameters := map[string]interface{}{
		"sampleCount": getIntValue(opts.N, 1),
	}

	// Add aspect ratio if specified.
	// Prefer opts.AspectRatio directly; fall back to converting from opts.Size.
	if aspectRatio := resolveAspectRatio(opts.AspectRatio, opts.Size); aspectRatio != "" {
		parameters["aspectRatio"] = aspectRatio
	}

	// sampleImageSize provider option (e.g., VertexImageSize1K, VertexImageSize2K)
	if imageSize := resolveVertexImageSize(opts.ProviderOptions); imageSize != "" {
		parameters["sampleImageSize"] = imageSize
	}

	// Seed for reproducible generation.
	if opts.Seed != nil {
		parameters["seed"] = *opts.Seed
	}

	// Additional Vertex AI provider options.
	if pgen, ok := vertexOpts["personGeneration"].(string); ok && pgen != "" {
		parameters["personGeneration"] = pgen
	}
	if safetySetting, ok := vertexOpts["safetySetting"].(string); ok && safetySetting != "" {
		parameters["safetySetting"] = safetySetting
	}
	if addWatermark, ok := vertexOpts["addWatermark"].(bool); ok {
		parameters["addWatermark"] = addWatermark
	}
	if storageUri, ok := vertexOpts["storageUri"].(string); ok && storageUri != "" {
		parameters["storageUri"] = storageUri
	}

	// TODO: Image editing support
	// To fully implement image editing, the provider.ImageGenerateOptions struct needs to be extended with:
	// - Files []ImageFile - reference images for editing
	// - Mask *ImageFile - mask image for inpainting/outpainting
	// - ProviderOptions map[string]interface{} - for provider-specific settings
	//
	// Vertex AI Imagen supports these edit modes:
	// - EDIT_MODE_INPAINT_INSERTION - Insert objects into masked region
	// - EDIT_MODE_INPAINT_REMOVAL - Remove objects from masked region
	// - EDIT_MODE_OUTPAINT - Extend image beyond boundaries
	// - EDIT_MODE_CONTROLLED_EDITING - Controlled editing with guidance
	// - EDIT_MODE_PRODUCT_IMAGE - Product-specific image editing
	// - EDIT_MODE_BGSWAP - Background replacement
	//
	// Example of what the editing request would look like:
	// if len(files) > 0 {
	//     referenceImages := []map[string]interface{}{}
	//     for i, file := range files {
	//         referenceImages = append(referenceImages, map[string]interface{}{
	//             "referenceType": "REFERENCE_TYPE_RAW",
	//             "referenceId": i + 1,
	//             "referenceImage": map[string]interface{}{
	//                 "bytesBase64Encoded": base64.StdEncoding.EncodeToString(file.Data),
	//             },
	//         })
	//     }
	//     if mask != nil {
	//         referenceImages = append(referenceImages, map[string]interface{}{
	//             "referenceType": "REFERENCE_TYPE_MASK",
	//             "referenceId": len(files) + 1,
	//             "referenceImage": map[string]interface{}{
	//                 "bytesBase64Encoded": base64.StdEncoding.EncodeToString(mask.Data),
	//             },
	//             "maskImageConfig": map[string]interface{}{
	//                 "maskMode": "MASK_MODE_USER_PROVIDED",
	//                 "dilation": 0.01,
	//             },
	//         })
	//     }
	//     instances[0]["referenceImages"] = referenceImages
	//     parameters["editMode"] = "EDIT_MODE_INPAINT_INSERTION"
	// }

	reqBody := map[string]interface{}{
		"instances":  []map[string]interface{}{instance},
		"parameters": parameters,
	}

	// Build URL
	path := fmt.Sprintf("/models/%s:predict", m.modelID)

	// Make request
	resp, err := m.prov.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to generate image: "+err.Error(), err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providererrors.NewProviderError("google-vertex", resp.StatusCode, "",
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(resp.Body)), nil)
	}

	// Parse response
	var imagenResp vertexImagenResponse
	if err := json.Unmarshal(resp.Body, &imagenResp); err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to parse response: "+err.Error(), err)
	}

	if len(imagenResp.Predictions) == 0 {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "no images in response", nil)
	}

	// Decode first image (Vertex AI returns base64)
	imageData, err := base64.StdEncoding.DecodeString(imagenResp.Predictions[0].BytesBase64Encoded)
	if err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to decode image: "+err.Error(), err)
	}

	return &types.ImageResult{
		Image:    imageData,
		MimeType: imagenResp.Predictions[0].MimeType,
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
	if imageSize := resolveVertexImageSize(opts.ProviderOptions); imageSize != "" {
		imageConfig["imageSize"] = imageSize
	}
	if len(imageConfig) > 0 {
		genConfig["imageConfig"] = imageConfig
	}

	// Build URL
	path := fmt.Sprintf("/models/%s:generateContent", m.modelID)

	// Make request
	resp, err := m.prov.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to generate image: "+err.Error(), err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providererrors.NewProviderError("google-vertex", resp.StatusCode, "",
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(resp.Body)), nil)
	}

	// Parse response
	var geminiResp vertexGeminiImageResponse
	if err := json.Unmarshal(resp.Body, &geminiResp); err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to parse response: "+err.Error(), err)
	}

	// Extract image from response
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "no image in response", nil)
	}

	// Find the image part (inlineData with image/* mimeType)
	var imageData []byte
	var mimeType string
	for _, part := range geminiResp.Candidates[0].Content.Parts {
		if part.InlineData != nil && len(part.InlineData.MimeType) >= 6 && part.InlineData.MimeType[:6] == "image/" {
			data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to decode image: "+err.Error(), err)
			}
			imageData = data
			mimeType = part.InlineData.MimeType
			break
		}
	}

	if imageData == nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "no image data in response", nil)
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

// extractVertexOptions returns the vertex-specific provider options map.
func extractVertexOptions(providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return map[string]interface{}{}
	}
	opts, _ := providerOptions["vertex"].(map[string]interface{})
	if opts == nil {
		return map[string]interface{}{}
	}
	return opts
}

// resolveVertexImageSize extracts the sampleImageSize option from provider options.
// Provider options format: map["vertex"]map["sampleImageSize"] = "1K"
// Valid values: VertexImageSize1K ("1K"), VertexImageSize2K ("2K").
func resolveVertexImageSize(providerOptions map[string]interface{}) string {
	opts := extractVertexOptions(providerOptions)
	size, _ := opts["sampleImageSize"].(string)
	return size
}

// getIntValue safely gets int value or default
func getIntValue(ptr *int, defaultVal int) int {
	if ptr != nil {
		return *ptr
	}
	return defaultVal
}

// Response types for Vertex AI Imagen API
type vertexImagenResponse struct {
	Predictions []struct {
		BytesBase64Encoded string `json:"bytesBase64Encoded"`
		MimeType           string `json:"mimeType"`
		Prompt             string `json:"prompt,omitempty"` // Revised prompt if available
	} `json:"predictions"`
}

// Response types for Vertex AI Gemini image API
type vertexGeminiImageResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string                  `json:"text,omitempty"`
				InlineData *vertexGeminiInlineData `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

type vertexGeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64-encoded
}

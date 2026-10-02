package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// imagenRemovedError matches TS google-image-model.ts / google-vertex-image-model.ts
// exactly: "Google image models other than Gemini are no longer supported. Use a
// model ID that starts with `gemini-`."
const imagenRemovedError = "Google image models other than Gemini are no longer supported. Use a model ID that starts with `gemini-`."

// ImageModel implements image generation for Google Vertex AI.
// Only Gemini image models (model IDs starting with "gemini-") are supported;
// Imagen (:predict API) was removed to match TS `ai@7.0.113`.
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
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "google-vertex"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// MaxImagesPerCall returns the maximum number of images generated per
// DoGenerate call. Gemini image models generate exactly one image per call;
// the core GenerateImage helper splits a larger request into multiple calls.
func (m *ImageModel) MaxImagesPerCall() int {
	return 1
}

// googleVertexImageModelsWithFileInputSupport lists Gemini image model IDs
// known to accept file inputs for image editing (TS
// GoogleVertexImageModel#supportsFileInputs).
var googleVertexImageModelsWithFileInputSupport = map[string]bool{
	"gemini-2.5-flash-image":         true,
	"gemini-3-pro-image-preview":     true,
	"gemini-3.1-flash-image-preview": true,
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if googleVertexImageModelsWithFileInputSupport[m.modelID] {
		return boolPtr(true)
	}
	return nil
}

// SupportsMaskInputs reports whether the model accepts a mask input for
// inpainting. Gemini image-capable models do not support masks.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if v := m.SupportsFileInputs(); v != nil && *v {
		return boolPtr(false)
	}
	return nil
}

func boolPtr(b bool) *bool {
	return &b
}

// DoGenerate performs image generation using Gemini models via the
// generateContent API. Non-Gemini model IDs are rejected, matching TS.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if !isGeminiModel(m.modelID) {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", imagenRemovedError, nil)
	}
	return m.doGenerateGemini(ctx, opts)
}

// doGenerateGemini generates images using Gemini models via the generateContent API
func (m *ImageModel) doGenerateGemini(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Image editing with masks is not supported for Gemini image models.
	if opts.Mask != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "Gemini image models do not support mask-based image editing.", nil)
	}
	// Gemini image models use the language model API with responseModalities: ["IMAGE"]
	genConfig := map[string]interface{}{
		"responseModalities": []string{"IMAGE"},
	}

	// Add seed if specified for reproducible generation.
	if opts.Seed != nil {
		genConfig["seed"] = *opts.Seed
	}

	parts := vertexGeminiImageParts(opts)
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role":  "user",
				"parts": parts,
			},
		},
		"generationConfig": genConfig,
	}

	// Add aspect ratio and/or imageSize if specified.
	imageConfig := map[string]interface{}{}
	if opts.AspectRatio != "" {
		imageConfig["aspectRatio"] = opts.AspectRatio
	}
	if imageSize := resolveVertexImageSize(opts.ProviderOptions); imageSize != "" {
		imageConfig["imageSize"] = imageSize
	}
	if len(imageConfig) > 0 {
		genConfig["imageConfig"] = imageConfig
	}
	for key, value := range extractVertexOptions(opts.ProviderOptions) {
		if value != nil {
			genConfig[key] = value
		}
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
	var base64Image string
	var mimeType string
	for _, part := range geminiResp.Candidates[0].Content.Parts {
		if part.InlineData != nil && len(part.InlineData.MimeType) >= 6 && part.InlineData.MimeType[:6] == "image/" {
			data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to decode image: "+err.Error(), err)
			}
			imageData = data
			base64Image = part.InlineData.Data
			mimeType = part.InlineData.MimeType
			break
		}
	}

	if imageData == nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "no image data in response", nil)
	}

	return &types.ImageResult{
		Image:        imageData,
		Images:       [][]byte{imageData},
		Base64Image:  base64Image,
		Base64Images: []string{base64Image},
		MimeType:     mimeType,
		Usage: types.ImageUsage{
			ImageCount: 1,
		},
		Warnings: vertexImageWarnings(opts),
		ProviderMetadata: map[string]interface{}{
			"googleVertex": map[string]interface{}{"images": []map[string]interface{}{{}}},
			"vertex":       map[string]interface{}{"images": []map[string]interface{}{{}}},
		},
	}, nil
}

func vertexGeminiImageParts(opts *provider.ImageGenerateOptions) []map[string]interface{} {
	parts := []map[string]interface{}{}
	if opts.Prompt != "" {
		parts = append(parts, map[string]interface{}{"text": opts.Prompt})
	}
	for _, file := range opts.Files {
		if file.Type == "url" || file.URL != "" {
			parts = append(parts, map[string]interface{}{
				"fileData": map[string]interface{}{
					"fileUri":  file.URL,
					"mimeType": "image/*",
				},
			})
			continue
		}
		parts = append(parts, map[string]interface{}{
			"inlineData": map[string]interface{}{
				"mimeType": file.MediaType,
				"data":     base64.StdEncoding.EncodeToString(file.Data),
			},
		})
	}
	return parts
}

func vertexImageWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	if opts == nil || opts.Size == "" {
		return nil
	}
	return []types.Warning{{
		Type:    "unsupported",
		Feature: "size",
		Details: "This model does not support the `size` option. Use `aspectRatio` instead.",
	}}
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
	opts, _ := providerOptions["googleVertex"].(map[string]interface{})
	if opts == nil {
		opts, _ = providerOptions["vertex"].(map[string]interface{})
	}
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

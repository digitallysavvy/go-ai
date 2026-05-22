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

	parameters := map[string]interface{}{}
	if opts.N != nil {
		parameters["sampleCount"] = *opts.N
	}

	// Add aspect ratio if specified.
	// Vertex Imagen follows the TypeScript SDK: size is accepted by the shared
	// image API but not serialized for this provider.
	if opts.AspectRatio != "" {
		parameters["aspectRatio"] = opts.AspectRatio
	}

	// sampleImageSize provider option (e.g., VertexImageSize1K, VertexImageSize2K)
	if imageSize := resolveVertexImageSize(opts.ProviderOptions); imageSize != "" {
		parameters["sampleImageSize"] = imageSize
	}

	// Seed for reproducible generation.
	if opts.Seed != nil {
		parameters["seed"] = *opts.Seed
	}

	// Additional Vertex AI provider options. TS spreads top-level image options
	// into parameters after standard fields; edit options are handled below.
	for key, value := range vertexOpts {
		if key == "edit" || value == nil {
			continue
		}
		parameters[key] = value
	}

	if len(opts.Files) > 0 {
		referenceImages, err := buildVertexReferenceImages(opts.Files, opts.Mask, vertexOpts)
		if err != nil {
			return nil, err
		}
		instance["referenceImages"] = referenceImages
		parameters["editMode"] = vertexEditString(vertexOpts, "mode", "EDIT_MODE_INPAINT_INSERTION")
		if baseSteps, ok := vertexEditNumber(vertexOpts, "baseSteps"); ok {
			parameters["editConfig"] = map[string]interface{}{"baseSteps": baseSteps}
		}
	} else if opts.Mask != nil {
		return nil, fmt.Errorf("google vertex imagen image editing requires at least one source image when mask is provided")
	}

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

	images := make([][]byte, 0, len(imagenResp.Predictions))
	base64Images := make([]string, 0, len(imagenResp.Predictions))
	imageMetadata := make([]map[string]interface{}, 0, len(imagenResp.Predictions))
	for _, prediction := range imagenResp.Predictions {
		imageData, err := base64.StdEncoding.DecodeString(prediction.BytesBase64Encoded)
		if err != nil {
			return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to decode image: "+err.Error(), err)
		}
		images = append(images, imageData)
		base64Images = append(base64Images, prediction.BytesBase64Encoded)
		meta := map[string]interface{}{}
		if prediction.Prompt != "" {
			meta["revisedPrompt"] = prediction.Prompt
		}
		imageMetadata = append(imageMetadata, meta)
	}
	metadata := map[string]interface{}{"images": imageMetadata}

	return &types.ImageResult{
		Image:        images[0],
		Images:       images,
		Base64Image:  base64Images[0],
		Base64Images: base64Images,
		MimeType:     imagenResp.Predictions[0].MimeType,
		Usage: types.ImageUsage{
			ImageCount: len(imagenResp.Predictions),
		},
		Warnings: vertexImageWarnings(opts),
		ProviderMetadata: map[string]interface{}{
			"googleVertex": metadata,
			"vertex":       metadata,
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
		return nil, fmt.Errorf("LGemini image models do not support generating multiple images. Use Imagen models for multiple image generation")
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

func buildVertexReferenceImages(files []provider.ImageFile, mask *provider.ImageFile, vertexOpts map[string]interface{}) ([]map[string]interface{}, error) {
	referenceImages := make([]map[string]interface{}, 0, len(files)+1)
	for i, file := range files {
		data, err := vertexImageFileBase64(file)
		if err != nil {
			return nil, err
		}
		referenceImages = append(referenceImages, map[string]interface{}{
			"referenceType": "REFERENCE_TYPE_RAW",
			"referenceId":   i + 1,
			"referenceImage": map[string]interface{}{
				"bytesBase64Encoded": data,
			},
		})
	}
	if mask != nil {
		data, err := vertexImageFileBase64(*mask)
		if err != nil {
			return nil, err
		}
		maskConfig := map[string]interface{}{
			"maskMode": vertexEditString(vertexOpts, "maskMode", "MASK_MODE_USER_PROVIDED"),
		}
		if dilation, ok := vertexEditNumber(vertexOpts, "maskDilation"); ok {
			maskConfig["dilation"] = dilation
		}
		referenceImages = append(referenceImages, map[string]interface{}{
			"referenceType": "REFERENCE_TYPE_MASK",
			"referenceId":   len(files) + 1,
			"referenceImage": map[string]interface{}{
				"bytesBase64Encoded": data,
			},
			"maskImageConfig": maskConfig,
		})
	}
	return referenceImages, nil
}

func vertexImageFileBase64(file provider.ImageFile) (string, error) {
	if file.Type == "url" || file.URL != "" {
		return "", fmt.Errorf("url-based images are not supported for Google Vertex image editing; provide image data directly")
	}
	if len(file.Data) == 0 {
		return "", fmt.Errorf("google vertex image editing requires non-empty image data")
	}
	return base64.StdEncoding.EncodeToString(file.Data), nil
}

func vertexEditOptions(vertexOpts map[string]interface{}) map[string]interface{} {
	edit, _ := vertexOpts["edit"].(map[string]interface{})
	if edit == nil {
		return map[string]interface{}{}
	}
	return edit
}

func vertexEditString(vertexOpts map[string]interface{}, key string, defaultValue string) string {
	if value, ok := vertexEditOptions(vertexOpts)[key].(string); ok && value != "" {
		return value
	}
	return defaultValue
}

func vertexEditNumber(vertexOpts map[string]interface{}, key string) (float64, bool) {
	switch value := vertexEditOptions(vertexOpts)[key].(type) {
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case float64:
		return value, true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
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

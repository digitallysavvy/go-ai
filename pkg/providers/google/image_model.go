package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
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
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	if m.prov == nil {
		return "google.generative-ai"
	}
	return m.prov.Name()
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

	aspectRatio := opts.AspectRatio
	if aspectRatio == "" {
		aspectRatio = ImageAspectRatio1x1
	}

	parameters := map[string]interface{}{
		"sampleCount": getIntValue(opts.N, 1),
		"aspectRatio": aspectRatio,
	}

	for key, value := range extractGoogleOptions(opts.ProviderOptions) {
		if key == "googleSearch" {
			continue
		}
		if value != nil {
			parameters[key] = value
		}
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
	path := fmt.Sprintf("/models/%s:predict", m.modelID)

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

	images := make([][]byte, 0, len(imagenResp.Predictions))
	base64Images := make([]string, 0, len(imagenResp.Predictions))
	imageMetadata := make([]map[string]interface{}, 0, len(imagenResp.Predictions))
	for _, prediction := range imagenResp.Predictions {
		imageData, err := base64.StdEncoding.DecodeString(prediction.BytesBase64Encoded)
		if err != nil {
			return nil, providererrors.NewProviderError("google", 0, "", "failed to decode image: "+err.Error(), err)
		}
		images = append(images, imageData)
		base64Images = append(base64Images, prediction.BytesBase64Encoded)
		meta := map[string]interface{}{}
		if prediction.Prompt != "" {
			meta["revisedPrompt"] = prediction.Prompt
		}
		imageMetadata = append(imageMetadata, meta)
	}

	return &types.ImageResult{
		Image:        images[0],
		Images:       images,
		Base64Image:  base64Images[0],
		Base64Images: base64Images,
		MimeType:     "image/png",
		Usage: types.ImageUsage{
			ImageCount: len(imagenResp.Predictions),
		},
		Warnings: googleImageWarnings(opts, true),
		ProviderMetadata: map[string]interface{}{
			"google": map[string]interface{}{"images": imageMetadata},
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

	providerOptions, googleSearch := geminiImageProviderOptions(opts)
	lmOpts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{
				Role:    types.RoleUser,
				Content: googleGeminiImageContent(opts),
			}},
		},
		Seed:            opts.Seed,
		Headers:         opts.Headers,
		ProviderOptions: providerOptions,
	}
	if googleSearch != nil {
		googleSearchArgs, _ := googleSearch.(map[string]interface{})
		lmOpts.Tools = []types.Tool{gemini.GoogleSearchTool(googleSearchArgs)}
	}

	lmResult, err := NewLanguageModel(m.prov, m.modelID).DoGenerate(ctx, lmOpts)
	if err != nil {
		return nil, err
	}
	file, ok := firstGeneratedImage(lmResult.Content)
	if !ok {
		return nil, providererrors.NewProviderError("google", 0, "", "no image data in response", nil)
	}
	base64Image := base64.StdEncoding.EncodeToString(file.Data)
	usage := types.ImageUsage{ImageCount: 1}
	if lmResult.Usage.InputTokens != nil {
		usage.InputTokens = int(*lmResult.Usage.InputTokens)
	}
	if lmResult.Usage.OutputTokens != nil {
		usage.OutputTokens = int(*lmResult.Usage.OutputTokens)
	}
	if lmResult.Usage.TotalTokens != nil {
		usage.TotalTokens = int(*lmResult.Usage.TotalTokens)
	}

	warnings := googleImageWarnings(opts, false)
	warnings = append(warnings, lmResult.Warnings...)
	return &types.ImageResult{
		Image:        file.Data,
		Images:       [][]byte{file.Data},
		Base64Image:  base64Image,
		Base64Images: []string{base64Image},
		MimeType:     file.MediaType,
		Usage:        usage,
		Warnings:     warnings,
		ProviderMetadata: map[string]interface{}{
			"google": googleImageProviderMetadata(lmResult.ProviderMetadata),
		},
		Response: lmResult.ResponseMetadata,
	}, nil
}

func googleGeminiImageContent(opts *provider.ImageGenerateOptions) []types.ContentPart {
	parts := []types.ContentPart{}
	if opts.Prompt != "" {
		parts = append(parts, types.TextContent{Text: opts.Prompt})
	}
	for _, file := range opts.Files {
		mediaType := file.MediaType
		if mediaType == "" {
			mediaType = "image/*"
		}
		parts = append(parts, types.FileContent{
			Data:      file.Data,
			MediaType: mediaType,
			MimeType:  mediaType,
			URL:       file.URL,
		})
	}
	return parts
}

func geminiImageProviderOptions(opts *provider.ImageGenerateOptions) (map[string]interface{}, interface{}) {
	googleOpts := map[string]interface{}{
		"responseModalities": []string{"IMAGE"},
	}
	imageConfig := map[string]interface{}{}
	if opts.AspectRatio != "" {
		imageConfig["aspectRatio"] = opts.AspectRatio
	}
	for key, value := range extractGoogleOptions(opts.ProviderOptions) {
		if key == "googleSearch" {
			continue
		}
		if key == "imageSize" {
			if value != nil {
				imageConfig["imageSize"] = value
			}
			continue
		}
		if value != nil {
			googleOpts[key] = value
		}
	}
	if len(imageConfig) > 0 {
		googleOpts["imageConfig"] = imageConfig
	}
	googleSearch := extractGoogleOptions(opts.ProviderOptions)["googleSearch"]
	return map[string]interface{}{"google": googleOpts}, googleSearch
}

func firstGeneratedImage(content []types.ContentPart) (types.GeneratedFileContent, bool) {
	for _, part := range content {
		file, ok := part.(types.GeneratedFileContent)
		if !ok || !strings.HasPrefix(file.MediaType, "image/") || len(file.Data) == 0 {
			continue
		}
		return file, true
	}
	return types.GeneratedFileContent{}, false
}

func googleImageProviderMetadata(providerMetadata map[string]interface{}) map[string]interface{} {
	googleMetadata := map[string]interface{}{"images": []map[string]interface{}{{}}}
	raw, ok := providerMetadata["google"]
	if !ok {
		return googleMetadata
	}
	switch meta := raw.(type) {
	case map[string]json.RawMessage:
		for key, value := range meta {
			googleMetadata[key] = value
		}
	case map[string]interface{}:
		for key, value := range meta {
			googleMetadata[key] = value
		}
	}
	if _, ok := googleMetadata["images"]; !ok {
		googleMetadata["images"] = []map[string]interface{}{{}}
	}
	return googleMetadata
}

func googleImageWarnings(opts *provider.ImageGenerateOptions, imagen bool) []types.Warning {
	if opts == nil {
		return nil
	}
	var warnings []types.Warning
	if opts.Size != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "size",
			Details: "This model does not support the `size` option. Use `aspectRatio` instead.",
		})
	}
	if imagen {
		if _, ok := extractGoogleOptions(opts.ProviderOptions)["googleSearch"]; ok {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "googleSearch",
				Details: "Google Search grounding is only supported on Gemini image models.",
			})
		}
	}
	if imagen && opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
			Details: "This model does not support the `seed` option through this provider.",
		})
	}
	return warnings
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
	googleOpts := extractGoogleOptions(providerOptions)
	if googleOpts == nil {
		return ""
	}
	val, _ := googleOpts[key].(string)
	return val
}

func extractGoogleOptions(providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return nil
	}
	googleOpts, ok := providerOptions["google"].(map[string]interface{})
	if !ok {
		return nil
	}
	return googleOpts
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

package stability

import (
	"context"
	"encoding/base64"
	"fmt"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Stability AI
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Stability AI image generation model
func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "stability"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	currentDate := time.Now()
	reqBody := m.buildRequestBody(opts)

	path := fmt.Sprintf("/v1/generation/%s/text-to-image", m.modelID)
	var response stabilityImageResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{Method: "POST", Path: path, Body: reqBody, Headers: opts.Headers}, &response)
	if err != nil {
		return nil, err
	}

	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = unsupportedWarnings(opts)
	result.Response = &types.ResponseMetadata{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   providerutils.ExtractHeaders(httpResp.Headers),
	}
	return result, nil
}

// unsupportedWarnings reports the call options the v1 text-to-image endpoint
// cannot honor, instead of silently dropping them.
func unsupportedWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "aspectRatio", Details: "Stability AI text-to-image does not support aspectRatio. Use size instead."})
	}
	if len(opts.Files) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "files", Details: "Stability AI text-to-image does not support input images. The files were ignored."})
	}
	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "mask", Details: "Stability AI text-to-image does not support masks. The mask was ignored."})
	}
	return warnings
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"text_prompts": []map[string]interface{}{
			{
				"text":   opts.Prompt,
				"weight": 1,
			},
		},
	}

	if opts.Size != "" {
		var width, height int
		if _, err := fmt.Sscanf(opts.Size, "%dx%d", &width, &height); err == nil && width > 0 && height > 0 {
			body["width"] = width
			body["height"] = height
		}
	}

	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	if opts.N != nil {
		body["samples"] = *opts.N
	} else {
		body["samples"] = 1
	}

	return body
}

func (m *ImageModel) convertResponse(response stabilityImageResponse) (*types.ImageResult, error) {
	if len(response.Artifacts) == 0 {
		return nil, fmt.Errorf("no image data returned from Stability AI")
	}

	images := make([][]byte, 0, len(response.Artifacts))
	b64 := make([]string, 0, len(response.Artifacts))
	for _, artifact := range response.Artifacts {
		imageBytes, err := base64.StdEncoding.DecodeString(artifact.Base64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 image: %w", err)
		}
		images = append(images, imageBytes)
		b64 = append(b64, artifact.Base64)
	}

	return &types.ImageResult{
		Image:        images[0],
		Images:       images,
		Base64Image:  b64[0],
		Base64Images: b64,
		MimeType:     "image/png",
		Usage: types.ImageUsage{
			ImageCount: len(response.Artifacts),
		},
	}, nil
}

type stabilityImageResponse struct {
	Artifacts []struct {
		Base64       string `json:"base64"`
		FinishReason string `json:"finishReason"`
		Seed         int    `json:"seed"`
	} `json:"artifacts"`
}

package prodia

import (
	"context"
	"encoding/json"
	"fmt"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Prodia
type ImageModel struct {
	prov    *Provider
	modelID string
}

// NewImageModel creates a new Prodia image generation model
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

// Provider returns the provider identifier for this model type.
// Matches the TypeScript SDK's config.provider value: "prodia.image".
func (m *ImageModel) Provider() string {
	return "prodia.image"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// ProdiaImageProviderOptions contains Prodia-specific image generation options.
type ProdiaImageProviderOptions struct {
	// Steps is the number of computational iterations (1–4).
	Steps *int `json:"steps,omitempty"`

	// Width of the output image in pixels (256–1920).
	Width *int `json:"width,omitempty"`

	// Height of the output image in pixels (256–1920).
	Height *int `json:"height,omitempty"`

	// StylePreset applies a visual theme to the output image.
	StylePreset *string `json:"stylePreset,omitempty"`

	// Loras specifies LoRA model augmentations (max 3).
	Loras []string `json:"loras,omitempty"`

	// Progressive returns a progressive JPEG when using JPEG output.
	Progressive *bool `json:"progressive,omitempty"`
}

// extractProviderOptions reads Prodia-specific options from the provider options map.
func extractProviderOptions(opts *provider.ImageGenerateOptions) *ProdiaImageProviderOptions {
	if opts.ProviderOptions == nil {
		return nil
	}
	raw, ok := opts.ProviderOptions["prodia"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var provOpts ProdiaImageProviderOptions
	if err := json.Unmarshal(b, &provOpts); err != nil {
		return nil
	}
	return &provOpts
}

// DoGenerate performs image generation using the Prodia API.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	var warnings []types.Warning

	// Warn about invalid size format — matches TS getArgs warning.
	if opts.Size != "" {
		var w, h int
		n, _ := fmt.Sscanf(opts.Size, "%dx%d", &w, &h)
		if n != 2 || w <= 0 || h <= 0 {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Message: fmt.Sprintf("Invalid size format: %s. Expected format: WIDTHxHEIGHT (e.g., 1024x1024)", opts.Size),
			})
		}
	}

	provOpts := extractProviderOptions(opts)

	reqBody := m.buildRequestBody(opts, provOpts)

	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/job",
		Body:    reqBody,
		Query:   m.buildQuery(),
		Headers: map[string]string{"Accept": "multipart/form-data; image/png"},
	})
	if err != nil {
		return nil, providererrors.NewProviderError("prodia", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("prodia API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	contentType := resp.Headers.Get("Content-Type")
	jobResp, imageData, mimeType, err := parseMultipartResponse(contentType, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse prodia response: %w", err)
	}

	if mimeType == "" {
		mimeType = "image/png"
	}

	result := &types.ImageResult{
		Image:    imageData,
		MimeType: mimeType,
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			"prodia": map[string]interface{}{
				"images": []interface{}{buildProdiaProviderMetadata(jobResp)},
			},
		},
	}
	if len(imageData) == 0 && jobResp.ImageURL != "" {
		result.URL = jobResp.ImageURL
	}

	return result, nil
}

// buildRequestBody builds the Prodia API request body.
func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, provOpts *ProdiaImageProviderOptions) map[string]interface{} {
	jobConfig := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	// Parse size (e.g., "1024x1024")
	if opts.Size != "" {
		var width, height int
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			jobConfig["width"] = width
			jobConfig["height"] = height
		}
	}

	if opts.Seed != nil {
		jobConfig["seed"] = *opts.Seed
	}

	if provOpts != nil {
		if provOpts.Width != nil {
			jobConfig["width"] = *provOpts.Width
		}
		if provOpts.Height != nil {
			jobConfig["height"] = *provOpts.Height
		}
		if provOpts.Steps != nil {
			jobConfig["steps"] = *provOpts.Steps
		}
		if provOpts.StylePreset != nil {
			jobConfig["style_preset"] = *provOpts.StylePreset
		}
		if len(provOpts.Loras) > 0 {
			jobConfig["loras"] = provOpts.Loras
		}
		if provOpts.Progressive != nil {
			jobConfig["progressive"] = *provOpts.Progressive
		}
	}

	return map[string]interface{}{
		"type":   m.modelID,
		"config": jobConfig,
	}
}

// buildQuery returns the URL query parameters for the Prodia request.
// price=true is always included to receive pricing information in the response.
func (m *ImageModel) buildQuery() map[string]string {
	return map[string]string{"price": "true"}
}


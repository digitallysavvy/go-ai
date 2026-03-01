package prodia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"

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

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "prodia"
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
	StylePreset *string `json:"style_preset,omitempty"`

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
	provOpts := extractProviderOptions(opts)

	reqBody := m.buildRequestBody(opts, provOpts)

	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method: "POST",
		Path:   "/job",
		Body:   reqBody,
		Query:  m.buildQuery(),
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

// parseMultipartResponse parses a multipart/form-data response from the Prodia API.
// It extracts the "job" JSON metadata part and the "output" binary image part.
// Returns the job metadata, image bytes, image MIME type, and any error.
func parseMultipartResponse(contentType string, body []byte) (*prodiaJobResponse, []byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to parse response Content-Type %q: %w", contentType, err)
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, nil, "", fmt.Errorf("multipart response missing boundary in Content-Type %q", contentType)
	}

	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var jobResp *prodiaJobResponse
	var imageData []byte
	var imageMIME string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to read multipart part: %w", err)
		}

		partData, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to read multipart part data: %w", err)
		}

		switch part.FormName() {
		case "job":
			var j prodiaJobResponse
			if err := json.Unmarshal(partData, &j); err != nil {
				return nil, nil, "", fmt.Errorf("failed to parse job metadata: %w", err)
			}
			jobResp = &j
		case "output":
			imageData = partData
			if ct := part.Header.Get("Content-Type"); ct != "" {
				if mt, _, err := mime.ParseMediaType(ct); err == nil {
					imageMIME = mt
				}
			}
		}
	}

	if jobResp == nil {
		return nil, nil, "", fmt.Errorf("multipart response missing 'job' part")
	}

	return jobResp, imageData, imageMIME, nil
}

// prodiaJobResponse represents the Prodia job API response metadata.
type prodiaJobResponse struct {
	ID       string `json:"id"`
	ImageURL string `json:"imageUrl,omitempty"`
}

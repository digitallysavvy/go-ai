package prodia

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	return "v4"
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

// SupportsFileInputs always reports false: Prodia's image models are
// text-to-image only (TS ProdiaImageModel#supportsFileInputs).
func (m *ImageModel) SupportsFileInputs() *bool {
	return boolPtr(false)
}

// SupportsMaskInputs always reports false, for the same reason as
// SupportsFileInputs.
func (m *ImageModel) SupportsMaskInputs() *bool {
	return boolPtr(false)
}

func boolPtr(b bool) *bool {
	return &b
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
	if err := validateImageOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	currentDate := time.Now()
	var warnings []types.Warning

	// Warn about invalid size format — matches TS getArgs warning.
	if opts.Size != "" {
		if _, _, ok := parseProdiaSize(opts.Size); !ok {
			msg := fmt.Sprintf("Invalid size format: %s. Expected format: WIDTHxHEIGHT (e.g., 1024x1024)", opts.Size)
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "size",
				Details: msg,
				Message: msg,
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
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "multipart/form-data; image/png"}, opts.Headers),
	})
	if err != nil {
		return nil, providererrors.NewProviderError("prodia", 0, "", err.Error(), err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newProdiaProviderError(resp.StatusCode, resp.Body, resp.Headers)
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
	result.Response = &types.ResponseMetadata{Timestamp: currentDate, ModelID: m.modelID, Headers: providerutils.ExtractHeaders(resp.Headers)}

	return result, nil
}

// jsNumberRe is a decimal JS Number() literal (surrounding whitespace is
// trimmed first).
var jsNumberRe = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// parseProdiaSize mirrors TS getArgs: size.split('x'), both parts non-empty
// and Number() finite; anything else is an invalid size. Zero, negative and
// fractional values are not invalid there.
func parseProdiaSize(size string) (width, height float64, ok bool) {
	parts := strings.Split(size, "x")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, false
	}
	num := func(s string) (float64, bool) {
		s = strings.TrimSpace(s)
		if !jsNumberRe.MatchString(s) {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil && !math.IsInf(f, 0)
	}
	w, wok := num(parts[0])
	h, hok := num(parts[1])
	if !wok || !hok {
		return 0, 0, false
	}
	return w, h, true
}

// prodiaNumber keeps a whole number an int (the wire and callers' view of
// 1024), and a fractional one a float64.
func prodiaNumber(f float64) interface{} {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return int(f)
	}
	return f
}

// buildRequestBody builds the Prodia API request body.
func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, provOpts *ProdiaImageProviderOptions) map[string]interface{} {
	jobConfig := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	// Parse size (e.g., "1024x1024")
	if opts.Size != "" {
		if width, height, ok := parseProdiaSize(opts.Size); ok {
			jobConfig["width"] = prodiaNumber(width)
			jobConfig["height"] = prodiaNumber(height)
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

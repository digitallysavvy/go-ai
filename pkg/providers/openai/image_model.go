package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for OpenAI DALL-E
type ImageModel struct {
	provider *Provider
	modelID  string
}

// OpenAIImageProviderOptions contains OpenAI image-generation specific options.
type OpenAIImageProviderOptions struct {
	Quality           string `json:"quality,omitempty"`
	Style             string `json:"style,omitempty"`
	Background        string `json:"background,omitempty"`
	Moderation        string `json:"moderation,omitempty"`
	OutputFormat      string `json:"outputFormat,omitempty"`
	OutputCompression *int   `json:"outputCompression,omitempty"`
	InputFidelity     string `json:"inputFidelity,omitempty"`
	User              string `json:"user,omitempty"`
}

// NewImageModel creates a new OpenAI image generation model
func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return m.provider.Name()
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
	if len(opts.Files) > 0 {
		if err := validateOpenAIImageProviderOptions(opts.ProviderOptions, true); err != nil {
			return nil, err
		}
		return m.doEdit(ctx, opts)
	}

	if err := validateOpenAIImageProviderOptions(opts.ProviderOptions, false); err != nil {
		return nil, err
	}
	reqBody := m.buildRequestBody(opts)
	var response openaiImageResponse
	err := m.provider.client.PostJSON(ctx, "/images/generations", reqBody, &response)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = imageWarnings(opts)
	return result, nil
}

// defaultResponseFormatPrefixes lists model ID prefixes that have their own
// default response format and should not receive an explicit "response_format"
// override. Setting response_format: "b64_json" on these models causes errors
// because they use a different default format (#12838).
var defaultResponseFormatPrefixes = []string{
	"chatgpt-image-",
	"gpt-image-1-mini",
	"gpt-image-1.5",
	"gpt-image-1",
	"gpt-image-2",
}

// hasDefaultResponseFormat returns true when the model has its own built-in
// response format and must not be sent an explicit response_format field.
func hasDefaultResponseFormat(modelID string) bool {
	for _, prefix := range defaultResponseFormatPrefixes {
		if len(modelID) >= len(prefix) && modelID[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	openaiOpts := extractOpenAIImageProviderOptions(opts.ProviderOptions)
	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	// Only set response_format for models that don't have a built-in default.
	// chatgpt-image and gpt-image-1 variants manage their own format (#12838).
	if !hasDefaultResponseFormat(m.modelID) {
		body["response_format"] = "b64_json"
	}
	if opts.N != nil {
		body["n"] = *opts.N
	}
	if opts.Size != "" {
		body["size"] = opts.Size
	}
	if quality := firstNonEmpty(openaiOpts.Quality, opts.Quality); quality != "" {
		body["quality"] = quality
	}
	if style := firstNonEmpty(openaiOpts.Style, opts.Style); style != "" {
		body["style"] = style
	}
	if openaiOpts.Background != "" {
		body["background"] = openaiOpts.Background
	}
	if openaiOpts.Moderation != "" {
		body["moderation"] = openaiOpts.Moderation
	}
	if openaiOpts.OutputFormat != "" {
		body["output_format"] = openaiOpts.OutputFormat
	}
	if openaiOpts.OutputCompression != nil {
		body["output_compression"] = *openaiOpts.OutputCompression
	}
	if openaiOpts.User != "" {
		body["user"] = openaiOpts.User
	}
	return body
}

func (m *ImageModel) doEdit(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	body, contentType, err := m.buildEditMultipartBody(ctx, opts)
	if err != nil {
		return nil, err
	}

	var response openaiImageResponse
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/images/edits",
		Headers: map[string]string{
			"Content-Type": contentType,
		},
		Body: body,
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = imageWarnings(opts)
	return result, nil
}

func (m *ImageModel) buildEditMultipartBody(ctx context.Context, opts *provider.ImageGenerateOptions) (io.Reader, string, error) {
	openaiOpts := extractOpenAIImageProviderOptions(opts.ProviderOptions)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		err := func() error {
			if err := writer.WriteField("model", m.modelID); err != nil {
				return err
			}
			if opts.Prompt != "" {
				if err := writer.WriteField("prompt", opts.Prompt); err != nil {
					return err
				}
			}
			if opts.N != nil {
				if err := writer.WriteField("n", fmt.Sprintf("%d", *opts.N)); err != nil {
					return err
				}
			}
			if opts.Size != "" {
				if err := writer.WriteField("size", opts.Size); err != nil {
					return err
				}
			}
			for key, value := range map[string]string{
				"quality":        openaiOpts.Quality,
				"background":     openaiOpts.Background,
				"output_format":  openaiOpts.OutputFormat,
				"input_fidelity": openaiOpts.InputFidelity,
				"user":           openaiOpts.User,
			} {
				if value != "" {
					if err := writer.WriteField(key, value); err != nil {
						return err
					}
				}
			}
			if openaiOpts.OutputCompression != nil {
				if err := writer.WriteField("output_compression", fmt.Sprintf("%d", *openaiOpts.OutputCompression)); err != nil {
					return err
				}
			}

			imageField := "image"
			if len(opts.Files) > 1 {
				imageField = "image[]"
			}
			for i, file := range opts.Files {
				if err := m.writeImageFilePart(ctx, writer, imageField, fmt.Sprintf("image-%d%s", i, imageExtension(file.MediaType)), file); err != nil {
					return err
				}
			}
			if opts.Mask != nil {
				if err := m.writeImageFilePart(ctx, writer, "mask", "mask"+imageExtension(opts.Mask.MediaType), *opts.Mask); err != nil {
					return err
				}
			}
			return writer.Close()
		}()
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	return pr, writer.FormDataContentType(), nil
}

func (m *ImageModel) writeImageFilePart(ctx context.Context, writer *multipart.Writer, fieldName, filename string, file provider.ImageFile) error {
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		return err
	}
	if file.Type == "url" && file.URL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
		if err != nil {
			return err
		}
		httpClient := http.DefaultClient
		if m.provider != nil && m.provider.client != nil && m.provider.client.HTTPClient() != nil {
			httpClient = m.provider.client.HTTPClient()
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode >= 400 {
			return fmt.Errorf("failed to download image %s: HTTP %d", file.URL, resp.StatusCode)
		}
		_, err = io.Copy(part, resp.Body)
		return err
	}
	_, err = part.Write(file.Data)
	return err
}

func imageExtension(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func imageWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	warnings := []types.Warning{}
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "This model does not support aspect ratio. Use `size` instead.",
		})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	return warnings
}

func extractOpenAIImageProviderOptions(providerOptions map[string]interface{}) OpenAIImageProviderOptions {
	if providerOptions == nil {
		return OpenAIImageProviderOptions{}
	}
	raw, ok := providerOptions["openai"]
	if !ok {
		return OpenAIImageProviderOptions{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return OpenAIImageProviderOptions{}
	}
	var opts OpenAIImageProviderOptions
	_ = json.Unmarshal(data, &opts)
	return opts
}

func validateOpenAIImageProviderOptions(providerOptions map[string]interface{}, edit bool) error {
	opts := extractOpenAIImageProviderOptions(providerOptions)
	if opts.Quality != "" && !oneOf(opts.Quality, "standard", "hd", "low", "medium", "high", "auto") {
		return fmt.Errorf("openai image provider option quality must be one of standard, hd, low, medium, high, auto")
	}
	if !edit && opts.Style != "" && !oneOf(opts.Style, "vivid", "natural") {
		return fmt.Errorf("openai image provider option style must be one of vivid, natural")
	}
	if opts.Background != "" && !oneOf(opts.Background, "transparent", "opaque", "auto") {
		return fmt.Errorf("openai image provider option background must be one of transparent, opaque, auto")
	}
	if !edit && opts.Moderation != "" && !oneOf(opts.Moderation, "auto", "low") {
		return fmt.Errorf("openai image provider option moderation must be one of auto, low")
	}
	if opts.OutputFormat != "" && !oneOf(opts.OutputFormat, "png", "jpeg", "webp") {
		return fmt.Errorf("openai image provider option outputFormat must be one of png, jpeg, webp")
	}
	if opts.OutputCompression != nil && (*opts.OutputCompression < 0 || *opts.OutputCompression > 100) {
		return fmt.Errorf("openai image provider option outputCompression must be between 0 and 100")
	}
	if edit && opts.InputFidelity != "" && !oneOf(opts.InputFidelity, "high", "low") {
		return fmt.Errorf("openai image provider option inputFidelity must be one of high, low")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (m *ImageModel) convertResponse(response openaiImageResponse) (*types.ImageResult, error) {
	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no image data returned from OpenAI")
	}
	imageData := response.Data[0]
	imageBytes, err := base64.StdEncoding.DecodeString(imageData.B64JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 image: %w", err)
	}
	return &types.ImageResult{
		Image:    imageBytes,
		MimeType: "image/png",
		URL:      imageData.URL,
		Usage: types.ImageUsage{
			ImageCount: len(response.Data),
		},
	}, nil
}

type openaiImageResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
}

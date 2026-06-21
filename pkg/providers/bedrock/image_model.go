package bedrock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for AWS Bedrock
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new AWS Bedrock image generation model
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
	return "amazon-bedrock"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// MaxImagesPerCall returns the maximum number of images accepted in one request.
func (m *ImageModel) MaxImagesPerCall() int {
	if m.modelID == "amazon.nova-canvas-v1:0" {
		return 5
	}
	return 1
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody, warnings, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("/model/%s/invoke", url.PathEscape(m.modelID))
	baseURL, err := m.provider.runtimeBaseURL()
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s%s", baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	requestHeaders := map[string]string{}
	if opts != nil {
		for k, v := range opts.Headers {
			requestHeaders[k] = v
		}
	}
	m.provider.applyRequestHeaders(req, requestHeaders)

	if err := m.provider.authenticateRequest(ctx, req, bodyBytes); err != nil {
		return nil, err
	}

	// Make the request using provider-scoped transport.
	resp, err := m.provider.Client().HTTPClient().Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError("amazon-bedrock", 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("AWS Bedrock API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return m.convertResponse(respBody, resp.Header, warnings)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) (map[string]interface{}, []types.Warning, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	warnings := []types.Warning{}
	options := bedrockImageOptions(opts.ProviderOptions)

	imageGenerationConfig := map[string]interface{}{}
	if opts.Size != "" {
		var width, height int
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 {
			imageGenerationConfig["width"] = width
		}
		if height > 0 {
			imageGenerationConfig["height"] = height
		}
	}
	if opts.Seed != nil && *opts.Seed != 0 {
		imageGenerationConfig["seed"] = *opts.Seed
	}
	if opts.N != nil && *opts.N != 0 {
		imageGenerationConfig["numberOfImages"] = *opts.N
	}
	if quality, ok := options["quality"].(string); ok && quality != "" {
		imageGenerationConfig["quality"] = quality
	}
	if cfgScale, ok := nonZeroNumberOption(options["cfgScale"]); ok {
		imageGenerationConfig["cfgScale"] = cfgScale
	}

	var args map[string]interface{}
	if len(opts.Files) > 0 {
		hasMask := opts.Mask != nil && opts.Mask.Type != ""
		_, hasMaskPrompt := options["maskPrompt"]
		taskType := stringOption(options["taskType"])
		if taskType == "" {
			if hasMask || hasMaskPrompt {
				taskType = "INPAINTING"
			} else {
				taskType = "IMAGE_VARIATION"
			}
		}
		sourceImage, err := bedrockImageFileBase64(opts.Files[0])
		if err != nil {
			return nil, nil, err
		}
		switch taskType {
		case "INPAINTING":
			params := map[string]interface{}{"image": sourceImage}
			if opts.Prompt != "" {
				params["text"] = opts.Prompt
			}
			addStringOption(params, options, "negativeText")
			if hasMask {
				maskImage, err := bedrockImageFileBase64(*opts.Mask)
				if err != nil {
					return nil, nil, err
				}
				params["maskImage"] = maskImage
			} else if hasMaskPrompt {
				params["maskPrompt"] = options["maskPrompt"]
			}
			args = map[string]interface{}{"taskType": "INPAINTING", "inPaintingParams": params, "imageGenerationConfig": imageGenerationConfig}
		case "OUTPAINTING":
			params := map[string]interface{}{"image": sourceImage}
			if opts.Prompt != "" {
				params["text"] = opts.Prompt
			}
			addStringOption(params, options, "negativeText")
			addStringOption(params, options, "outPaintingMode")
			if hasMask {
				maskImage, err := bedrockImageFileBase64(*opts.Mask)
				if err != nil {
					return nil, nil, err
				}
				params["maskImage"] = maskImage
			} else if hasMaskPrompt {
				params["maskPrompt"] = options["maskPrompt"]
			}
			args = map[string]interface{}{"taskType": "OUTPAINTING", "outPaintingParams": params, "imageGenerationConfig": imageGenerationConfig}
		case "BACKGROUND_REMOVAL":
			args = map[string]interface{}{"taskType": "BACKGROUND_REMOVAL", "backgroundRemovalParams": map[string]interface{}{"image": sourceImage}}
		case "IMAGE_VARIATION":
			images := make([]string, 0, len(opts.Files))
			for _, file := range opts.Files {
				image, err := bedrockImageFileBase64(file)
				if err != nil {
					return nil, nil, err
				}
				images = append(images, image)
			}
			params := map[string]interface{}{"images": images}
			if opts.Prompt != "" {
				params["text"] = opts.Prompt
			}
			addStringOption(params, options, "negativeText")
			if similarityStrength, ok := numberOption(options["similarityStrength"]); ok {
				params["similarityStrength"] = similarityStrength
			}
			args = map[string]interface{}{"taskType": "IMAGE_VARIATION", "imageVariationParams": params, "imageGenerationConfig": imageGenerationConfig}
		default:
			return nil, nil, fmt.Errorf("unsupported task type: %s", taskType)
		}
	} else {
		params := map[string]interface{}{"text": opts.Prompt}
		addStringOption(params, options, "negativeText")
		if style := stringOption(options["style"]); style != "" {
			params["style"] = style
		}
		args = map[string]interface{}{"taskType": "TEXT_IMAGE", "textToImageParams": params, "imageGenerationConfig": imageGenerationConfig}
	}

	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "This model does not support aspect ratio. Use `size` instead.",
		})
	}
	return args, warnings, nil
}

func (m *ImageModel) convertResponse(body []byte, headers http.Header, warnings []types.Warning) (*types.ImageResult, error) {
	var response struct {
		Images  []string               `json:"images"`
		ID      string                 `json:"id"`
		Status  string                 `json:"status"`
		Details map[string]interface{} `json:"details"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status == "Request Moderated" {
		reasons := []string{"Unknown"}
		if rawReasons, ok := response.Details["Moderation Reasons"].([]interface{}); ok && len(rawReasons) > 0 {
			reasons = reasons[:0]
			for _, reason := range rawReasons {
				reasons = append(reasons, fmt.Sprint(reason))
			}
		}
		return nil, fmt.Errorf("Amazon Bedrock request was moderated: %s", strings.Join(reasons, ", "))
	}
	if len(response.Images) == 0 {
		message := "Amazon Bedrock returned no images."
		if response.Status != "" {
			message += " Status: " + response.Status
		}
		return nil, fmt.Errorf("%s", message)
	}

	images := make([][]byte, 0, len(response.Images))
	for _, image := range response.Images {
		images = append(images, []byte(image))
	}
	return &types.ImageResult{
		Image:        images[0],
		Images:       images,
		Base64Image:  response.Images[0],
		Base64Images: response.Images,
		MimeType:     "image/png",
		Usage:        types.ImageUsage{},
		Warnings:     warnings,
		Response:     &types.ResponseMetadata{ID: response.ID, Timestamp: time.Now(), ModelID: m.modelID, Headers: flattenHeaders(headers)},
	}, nil
}

func bedrockImageOptions(providerOptions map[string]interface{}) map[string]interface{} {
	for _, key := range []string{"amazonBedrock", "bedrock"} {
		raw, ok := providerOptions[key]
		if !ok {
			continue
		}
		if values, ok := raw.(map[string]interface{}); ok {
			return values
		}
		return nil
	}
	return nil
}

func bedrockImageFileBase64(file provider.ImageFile) (string, error) {
	if file.Type == "url" || file.URL != "" {
		return "", fmt.Errorf("URL-based images are not supported for Amazon Bedrock image editing. Please provide the image data directly")
	}
	if alreadyBase64(file.Data) {
		return string(file.Data), nil
	}
	return base64.StdEncoding.EncodeToString(file.Data), nil
}

func alreadyBase64(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return false
	}
	return base64.StdEncoding.EncodeToString(decoded) == trimmed
}

func addStringOption(target map[string]interface{}, options map[string]interface{}, key string) {
	if value := stringOption(options[key]); value != "" {
		target[key] = value
	}
}

func stringOption(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func numberOption(value interface{}) (interface{}, bool) {
	switch v := value.(type) {
	case int, int32, int64, float32, float64, json.Number:
		return v, true
	default:
		return nil, false
	}
}

func nonZeroNumberOption(value interface{}) (interface{}, bool) {
	number, ok := numberOption(value)
	if !ok {
		return nil, false
	}
	switch v := value.(type) {
	case int:
		return number, v != 0
	case int32:
		return number, v != 0
	case int64:
		return number, v != 0
	case float32:
		return number, v != 0
	case float64:
		return number, v != 0
	case json.Number:
		f, err := v.Float64()
		return number, err != nil || f != 0
	default:
		return number, true
	}
}

func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

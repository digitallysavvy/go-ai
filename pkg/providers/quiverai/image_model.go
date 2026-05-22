package quiverai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const (
	ModelArrow1     = "arrow-1"
	ModelArrow11    = "arrow-1.1"
	ModelArrow11Max = "arrow-1.1-max"

	OperationGenerate  = "generate"
	OperationVectorize = "vectorize"
)

// ImageModel implements QuiverAI SVG generation and vectorization.
type ImageModel struct {
	provider *Provider
	modelID  string
}

func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{provider: provider, modelID: modelID}
}

func (m *ImageModel) SpecificationVersion() string { return "v4" }

func (m *ImageModel) Provider() string { return "quiverai.image" }

func (m *ImageModel) ModelID() string { return m.modelID }

func (m *ImageModel) MaxImagesPerCall() int { return 16 }

func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	operation := quiverOptionString(opts.ProviderOptions, "operation")
	if operation == "" {
		operation = OperationGenerate
	}
	body, err := m.buildRequestBody(opts, operation)
	if err != nil {
		return nil, err
	}
	var response svgGenerationResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    operationPath(operation),
		Body:    body,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	if len(response.Data) == 0 {
		return nil, providererrors.NewProviderError("quiverai", 0, "", "no SVGs in response", nil)
	}
	images := make([][]byte, len(response.Data))
	imageMetadata := make([]map[string]interface{}, len(response.Data))
	for i, image := range response.Data {
		images[i] = []byte(image.SVG)
		imageMetadata[i] = map[string]interface{}{
			"index":    i,
			"mimeType": image.MimeType,
		}
	}
	timestamp := time.Now()
	if response.Created > 0 {
		timestamp = time.Unix(response.Created, 0)
	}
	usage := types.ImageUsage{ImageCount: len(images)}
	if response.Usage != nil {
		usage.InputTokens = response.Usage.InputTokens
		usage.OutputTokens = response.Usage.OutputTokens
		usage.TotalTokens = response.Usage.TotalTokens
	}
	result := &types.ImageResult{
		Image:    images[0],
		Images:   images,
		MimeType: "image/svg+xml",
		Usage:    usage,
		Warnings: quiverWarnings(opts),
		ProviderMetadata: map[string]interface{}{
			"quiverai": map[string]interface{}{"images": imageMetadata},
		},
		Response: &types.ResponseMetadata{
			ID:        response.ID,
			Timestamp: timestamp,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
		},
	}
	return result, nil
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, operation string) (map[string]interface{}, error) {
	shared := map[string]interface{}{
		"model":             m.modelID,
		"n":                 imageCount(opts.N),
		"temperature":       quiverOption(opts.ProviderOptions, "temperature"),
		"top_p":             quiverOption(opts.ProviderOptions, "topP"),
		"presence_penalty":  quiverOption(opts.ProviderOptions, "presencePenalty"),
		"max_output_tokens": quiverOption(opts.ProviderOptions, "maxOutputTokens"),
		"stream":            false,
	}
	if operation == OperationGenerate {
		if strings.TrimSpace(opts.Prompt) == "" {
			return nil, fmt.Errorf("quiverai: generate requires a non-empty prompt")
		}
		references := make([]map[string]string, 0, len(opts.Files))
		maxReferences := 4
		if m.modelID == ModelArrow11Max {
			maxReferences = 16
		}
		if len(opts.Files) > maxReferences {
			return nil, fmt.Errorf("quiverai: generate supports up to %d reference images for model %q", maxReferences, m.modelID)
		}
		for _, file := range opts.Files {
			references = append(references, quiverImageReference(file))
		}
		shared["prompt"] = opts.Prompt
		shared["instructions"] = quiverOption(opts.ProviderOptions, "instructions")
		if len(references) > 0 {
			shared["references"] = references
		}
		return pruneNil(shared), nil
	}
	if operation != OperationVectorize {
		return nil, fmt.Errorf("quiverai: unsupported operation %q", operation)
	}
	if len(opts.Files) == 0 {
		return nil, fmt.Errorf("quiverai: vectorize requires one input image")
	}
	if len(opts.Files) > 1 {
		return nil, fmt.Errorf("quiverai: vectorize accepts a single input image")
	}
	shared["image"] = quiverImageReference(opts.Files[0])
	shared["auto_crop"] = quiverOption(opts.ProviderOptions, "autoCrop")
	shared["target_size"] = quiverOption(opts.ProviderOptions, "targetSize")
	return pruneNil(shared), nil
}

func operationPath(operation string) string {
	if operation == OperationVectorize {
		return "/svgs/vectorizations"
	}
	return "/svgs/generations"
}

func imageCount(n *int) int {
	if n == nil || *n == 0 {
		return 1
	}
	return *n
}

func quiverImageReference(file provider.ImageFile) map[string]string {
	if file.Type == "url" || file.URL != "" {
		return map[string]string{"url": file.URL}
	}
	return map[string]string{"base64": base64.StdEncoding.EncodeToString(file.Data)}
}

func quiverOption(providerOptions map[string]interface{}, key string) interface{} {
	opts, ok := providerOptions["quiverai"].(map[string]interface{})
	if !ok {
		return nil
	}
	return opts[key]
}

func quiverOptionString(providerOptions map[string]interface{}, key string) string {
	value, _ := quiverOption(providerOptions, key).(string)
	return value
}

func pruneNil(values map[string]interface{}) map[string]interface{} {
	for key, value := range values {
		if value == nil {
			delete(values, key)
		}
	}
	return values
}

func quiverWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.Size != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "size", Details: "QuiverAI SVG generation does not support the `size` option. The setting was ignored."})
	}
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "aspectRatio", Details: "QuiverAI SVG generation does not support the `aspectRatio` option. The setting was ignored."})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed", Details: "QuiverAI SVG generation does not support the `seed` option. The setting was ignored."})
	}
	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "mask", Details: "QuiverAI SVG generation does not support masks. The mask was ignored."})
	}
	return warnings
}

func (m *ImageModel) handleError(err error) error {
	if statusErr, ok := err.(*internalhttp.HTTPStatusError); ok {
		var apiErr quiverAPIError
		if json.Unmarshal(statusErr.Body, &apiErr) == nil && apiErr.Message != "" {
			return providererrors.NewProviderError("quiverai", statusErr.StatusCode, apiErr.Code, apiErr.Message, err)
		}
		return providererrors.NewProviderError("quiverai", statusErr.StatusCode, "", string(statusErr.Body), err)
	}
	return providererrors.NewProviderError("quiverai", http.StatusInternalServerError, "", err.Error(), err)
}

type svgGenerationResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Data    []struct {
		SVG      string `json:"svg"`
		MimeType string `json:"mime_type"`
	} `json:"data"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type quiverAPIError struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

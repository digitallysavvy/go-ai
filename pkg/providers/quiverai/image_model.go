package quiverai

import (
	"context"
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
	ModelArrow1      = "arrow-1"
	ModelArrow11     = "arrow-1.1"
	ModelArrow11Max  = "arrow-1.1-max"
	ModelArrow2      = "arrow-2"
	ModelArrow2Telos = "arrow-2-telos"

	OperationGenerate  = "generate"
	OperationVectorize = "vectorize"
	OperationEdit      = "edit"
	OperationAnimate   = "animate"

	// maxArrow2OutputTokens is the Arrow 2 family's output-token ceiling.
	// Other models retain the legacy 131072 upper bound (validated in
	// image_model_options.go).
	maxArrow2OutputTokens = 65536
)

// ImageModel implements QuiverAI SVG generation, vectorization, editing, and
// animation.
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

// isArrow2Model reports whether modelID is one of the Arrow 2 family models,
// which support SVG editing/animation and a lower output-token ceiling.
func isArrow2Model(modelID string) bool {
	return modelID == ModelArrow2 || modelID == ModelArrow2Telos
}

// getGenerateReferenceLimit returns the maximum number of reference images
// accepted by the `generate` operation for modelID. Only the documented
// Arrow 1.x models (excluding 1.1-max) use the lower 4-image limit; the API
// enforces model-specific limits within its 16-reference request limit for
// everything else.
func getGenerateReferenceLimit(modelID string) int {
	switch modelID {
	case "arrow-1", "arrow-1.0", "arrow-1.1":
		return 4
	default:
		return 16
	}
}

func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	quiverOpts, err := parseQuiverAIImageModelOptions(quiverRawOptions(opts.ProviderOptions))
	if err != nil {
		return nil, err
	}
	operation := quiverOpts.Operation
	if operation == "" {
		operation = OperationGenerate
	}
	body, err := m.buildRequestBody(opts, operation, quiverOpts)
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
		meta := map[string]interface{}{
			"index":    i,
			"mimeType": image.MimeType,
		}
		if image.LoopPeriodMs != nil {
			meta["loopPeriodMs"] = *image.LoopPeriodMs
		}
		if image.OpeningAnimationMs != nil {
			meta["openingAnimationMs"] = *image.OpeningAnimationMs
		}
		imageMetadata[i] = meta
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

	// Fixed-credit billing metadata: the API reports a flat per-request credit
	// cost independent of token usage. Only surfaced when present, matching
	// the TS SDK's `...(response.credits != null && { credits: ... })` spread.
	quiverMeta := map[string]interface{}{"images": imageMetadata}
	if response.Credits != nil {
		quiverMeta["credits"] = *response.Credits
	}

	result := &types.ImageResult{
		Image:    images[0],
		Images:   images,
		MimeType: "image/svg+xml",
		Usage:    usage,
		Warnings: quiverWarnings(opts),
		ProviderMetadata: map[string]interface{}{
			"quiverai": quiverMeta,
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

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, operation string, quiverOpts *QuiverAIImageModelOptions) (map[string]interface{}, error) {
	if isArrow2Model(m.modelID) && quiverOpts.MaxOutputTokens != nil && *quiverOpts.MaxOutputTokens > maxArrow2OutputTokens {
		return nil, invalidQuiverArgument("providerOptions.quiverai.maxOutputTokens", fmt.Sprintf("QuiverAI model %q supports at most %d output tokens.", m.modelID, maxArrow2OutputTokens))
	}

	if operation != OperationEdit {
		if err := rejectEditOnlyOptions(operation, quiverOpts); err != nil {
			return nil, err
		}
	}

	switch operation {
	case OperationGenerate:
		return m.buildGenerateBody(opts, quiverOpts)
	case OperationEdit:
		return m.buildEditBody(opts, quiverOpts)
	case OperationAnimate:
		return m.buildAnimateBody(opts, quiverOpts)
	case OperationVectorize:
		return m.buildVectorizeBody(opts, quiverOpts)
	default:
		return nil, invalidQuiverArgument("providerOptions.quiverai.operation", fmt.Sprintf("unsupported operation %q", operation))
	}
}

// buildSharedOptions builds the option fields common to `generate` and
// `vectorize` requests, mirroring the TS SDK's sharedOptions object.
func buildSharedOptions(quiverOpts *QuiverAIImageModelOptions) map[string]interface{} {
	shared := map[string]interface{}{"stream": false}
	if quiverOpts.Temperature != nil {
		shared["temperature"] = *quiverOpts.Temperature
	}
	if quiverOpts.TopP != nil {
		shared["top_p"] = *quiverOpts.TopP
	}
	if quiverOpts.PresencePenalty != nil {
		shared["presence_penalty"] = *quiverOpts.PresencePenalty
	}
	if quiverOpts.MaxOutputTokens != nil {
		shared["max_output_tokens"] = *quiverOpts.MaxOutputTokens
	}
	if quiverOpts.ReasoningEffort != "" {
		shared["reasoning_effort"] = quiverOpts.ReasoningEffort
	}
	if quiverOpts.Attributes != nil {
		shared["attributes"] = attributesToWire(quiverOpts.Attributes)
	}
	return shared
}

func attributesToWire(attrs *QuiverAIAttributes) map[string]interface{} {
	result := map[string]interface{}{}
	if attrs.ViewBox != nil {
		result["viewBox"] = map[string]interface{}{
			"minX":   attrs.ViewBox.MinX,
			"minY":   attrs.ViewBox.MinY,
			"width":  attrs.ViewBox.Width,
			"height": attrs.ViewBox.Height,
		}
	}
	return result
}

func (m *ImageModel) buildGenerateBody(opts *provider.ImageGenerateOptions, quiverOpts *QuiverAIImageModelOptions) (map[string]interface{}, error) {
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, invalidQuiverArgument("prompt", "QuiverAI image generation requires a non-empty prompt for generateImage.")
	}
	maxReferences := getGenerateReferenceLimit(m.modelID)
	if len(opts.Files) > maxReferences {
		return nil, invalidQuiverArgument("files", fmt.Sprintf("QuiverAI generate supports up to %d reference images for model %q.", maxReferences, m.modelID))
	}

	body := map[string]interface{}{
		"model":  m.modelID,
		"n":      imageCount(opts.N),
		"prompt": opts.Prompt,
	}
	for k, v := range buildSharedOptions(quiverOpts) {
		body[k] = v
	}
	if quiverOpts.Instructions != "" {
		body["instructions"] = quiverOpts.Instructions
	}
	if len(opts.Files) > 0 {
		references := make([]map[string]interface{}, 0, len(opts.Files))
		for _, file := range opts.Files {
			references = append(references, toQuiverAIImageReference(file))
		}
		body["references"] = references
	}
	return body, nil
}

func (m *ImageModel) buildVectorizeBody(opts *provider.ImageGenerateOptions, quiverOpts *QuiverAIImageModelOptions) (map[string]interface{}, error) {
	if len(opts.Files) == 0 {
		return nil, invalidQuiverArgument("files", `QuiverAI vectorize requires an input image. Pass an image in the generateImage prompt and set providerOptions.quiverai.operation to "vectorize".`)
	}
	if len(opts.Files) > 1 {
		return nil, invalidQuiverArgument("files", "QuiverAI vectorize accepts a single input image.")
	}
	if n := imageCount(opts.N); n != 1 {
		return nil, invalidQuiverArgument("n", "QuiverAI vectorize returns one SVG per request. Set maxImagesPerCall to 1 in generateImage to vectorize multiple times.")
	}

	body := map[string]interface{}{
		"model": m.modelID,
		"image": toQuiverAIImageReference(opts.Files[0]),
	}
	for k, v := range buildSharedOptions(quiverOpts) {
		body[k] = v
	}
	if quiverOpts.AutoCrop != nil {
		body["auto_crop"] = *quiverOpts.AutoCrop
	}
	if quiverOpts.TargetSize != nil {
		body["target_size"] = *quiverOpts.TargetSize
	}
	return body, nil
}

func (m *ImageModel) buildEditBody(opts *provider.ImageGenerateOptions, quiverOpts *QuiverAIImageModelOptions) (map[string]interface{}, error) {
	if !isArrow2Model(m.modelID) {
		return nil, invalidQuiverArgument("modelId", `QuiverAI SVG editing is supported by the "arrow-2" and "arrow-2-telos" models.`)
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, invalidQuiverArgument("prompt", "QuiverAI SVG editing requires a non-empty instruction in generateImage prompt.text.")
	}
	if len(opts.Prompt) > 4000 {
		return nil, invalidQuiverArgument("prompt", "QuiverAI SVG editing instructions must contain at most 4000 characters.")
	}
	if len(opts.Files) == 0 {
		return nil, invalidQuiverArgument("files", "QuiverAI SVG editing requires one source SVG in generateImage prompt.images.")
	}
	if len(opts.Files) != 1 {
		return nil, invalidQuiverArgument("files", "QuiverAI SVG editing accepts exactly one source SVG.")
	}
	if n := imageCount(opts.N); n != 1 {
		return nil, invalidQuiverArgument("n", "QuiverAI SVG editing returns exactly one SVG per request. Set maxImagesPerCall to 1 in generateImage to edit multiple times.")
	}
	if opts.Mask != nil {
		return nil, invalidQuiverArgument("mask", "QuiverAI SVG editing does not support masks.")
	}

	var unsupported []string
	if quiverOpts.Instructions != "" {
		unsupported = append(unsupported, "instructions")
	}
	if quiverOpts.Attributes != nil {
		unsupported = append(unsupported, "attributes")
	}
	if quiverOpts.TopP != nil {
		unsupported = append(unsupported, "topP")
	}
	if quiverOpts.PresencePenalty != nil {
		unsupported = append(unsupported, "presencePenalty")
	}
	if quiverOpts.AutoCrop != nil {
		unsupported = append(unsupported, "autoCrop")
	}
	if quiverOpts.TargetSize != nil {
		unsupported = append(unsupported, "targetSize")
	}
	if len(unsupported) > 0 {
		return nil, invalidQuiverArgument("providerOptions", fmt.Sprintf("QuiverAI SVG editing does not support these provider options: %s.", strings.Join(unsupported, ", ")))
	}

	source, err := toQuiverAIEditSource(opts.Files[0])
	if err != nil {
		return nil, err
	}
	referenceImages, err := referenceImagesToWire(quiverOpts.ReferenceImages)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
		"stream": false,
	}
	for k, v := range source {
		body[k] = v
	}
	if referenceImages != nil {
		body["reference_images"] = referenceImages
	}
	if quiverOpts.MaxReviewSteps != nil {
		body["max_review_steps"] = *quiverOpts.MaxReviewSteps
	}
	if quiverOpts.ReasoningEffort != "" {
		body["reasoning_effort"] = quiverOpts.ReasoningEffort
	}
	settings := map[string]interface{}{}
	if quiverOpts.MaxOutputTokens != nil {
		settings["max_output_tokens"] = *quiverOpts.MaxOutputTokens
	}
	if quiverOpts.OrchestratorMaxOutputTokens != nil {
		settings["orchestrator_max_output_tokens"] = *quiverOpts.OrchestratorMaxOutputTokens
	}
	if quiverOpts.ShallowMaxOutputTokens != nil {
		settings["shallow_max_output_tokens"] = *quiverOpts.ShallowMaxOutputTokens
	}
	if quiverOpts.Temperature != nil {
		settings["temperature"] = *quiverOpts.Temperature
	}
	if len(settings) > 0 {
		body["settings"] = settings
	}
	return body, nil
}

func (m *ImageModel) buildAnimateBody(opts *provider.ImageGenerateOptions, quiverOpts *QuiverAIImageModelOptions) (map[string]interface{}, error) {
	if !isArrow2Model(m.modelID) {
		return nil, invalidQuiverArgument("modelId", `QuiverAI animate is supported by the "arrow-2" and "arrow-2-telos" models.`)
	}
	if len(opts.Files) == 0 {
		return nil, invalidQuiverArgument("files", "QuiverAI animate requires exactly one source SVG in prompt.images.")
	}
	if len(opts.Files) != 1 {
		return nil, invalidQuiverArgument("files", "QuiverAI animate accepts exactly one source SVG in prompt.images.")
	}
	if n := imageCount(opts.N); n != 1 {
		return nil, invalidQuiverArgument("n", "QuiverAI animate returns one SVG per request. Set maxImagesPerCall to 1 in generateImage to animate multiple times.")
	}
	if opts.Mask != nil {
		return nil, invalidQuiverArgument("mask", "QuiverAI animate does not support masks.")
	}
	// opts.Prompt == "" is treated as "no animation instruction provided" (Go
	// has no separate undefined/empty distinction here); whitespace-only text
	// is rejected the same way the TS SDK rejects an explicitly blank prompt.
	if opts.Prompt != "" && strings.TrimSpace(opts.Prompt) == "" {
		return nil, invalidQuiverArgument("prompt", "QuiverAI animate requires a non-empty prompt when an animation instruction is provided.")
	}

	switch {
	case quiverOpts.Instructions != "":
		return nil, invalidQuiverArgument("providerOptions.quiverai.instructions", "QuiverAI animate does not support providerOptions.quiverai.instructions.")
	case quiverOpts.TopP != nil:
		return nil, invalidQuiverArgument("providerOptions.quiverai.topP", "QuiverAI animate does not support providerOptions.quiverai.topP.")
	case quiverOpts.PresencePenalty != nil:
		return nil, invalidQuiverArgument("providerOptions.quiverai.presencePenalty", "QuiverAI animate does not support providerOptions.quiverai.presencePenalty.")
	case quiverOpts.Attributes != nil:
		return nil, invalidQuiverArgument("providerOptions.quiverai.attributes", "QuiverAI animate does not support providerOptions.quiverai.attributes.")
	case quiverOpts.AutoCrop != nil:
		return nil, invalidQuiverArgument("providerOptions.quiverai.autoCrop", "QuiverAI animate does not support providerOptions.quiverai.autoCrop.")
	case quiverOpts.TargetSize != nil:
		return nil, invalidQuiverArgument("providerOptions.quiverai.targetSize", "QuiverAI animate does not support providerOptions.quiverai.targetSize.")
	}

	source, err := toQuiverAIAnimationSource(opts.Files[0])
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"model":      m.modelID,
		"svg_source": source,
		"stream":     false,
	}
	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}
	if quiverOpts.Temperature != nil {
		body["temperature"] = *quiverOpts.Temperature
	}
	if quiverOpts.MaxOutputTokens != nil {
		body["max_output_tokens"] = *quiverOpts.MaxOutputTokens
	}
	if quiverOpts.ReasoningEffort != "" {
		body["reasoning_effort"] = quiverOpts.ReasoningEffort
	}
	return body, nil
}

// rejectEditOnlyOptions rejects providerOptions.quiverai fields that only
// apply to the `edit` operation when a different operation is requested,
// mirroring the TS SDK's rejectEditOnlyOptions.
func rejectEditOnlyOptions(operation string, opts *QuiverAIImageModelOptions) error {
	var unsupported []string
	if len(opts.ReferenceImages) > 0 {
		unsupported = append(unsupported, "referenceImages")
	}
	if opts.MaxReviewSteps != nil {
		unsupported = append(unsupported, "maxReviewSteps")
	}
	if opts.OrchestratorMaxOutputTokens != nil {
		unsupported = append(unsupported, "orchestratorMaxOutputTokens")
	}
	if opts.ShallowMaxOutputTokens != nil {
		unsupported = append(unsupported, "shallowMaxOutputTokens")
	}
	if len(unsupported) > 0 {
		return invalidQuiverArgument("providerOptions", fmt.Sprintf("QuiverAI %s does not support these edit-only provider options: %s.", operation, strings.Join(unsupported, ", ")))
	}
	return nil
}

func operationPath(operation string) string {
	switch operation {
	case OperationVectorize:
		return "/svgs/vectorizations"
	case OperationEdit:
		return "/svgs/edits"
	case OperationAnimate:
		return "/svgs/animations"
	default:
		return "/svgs/generations"
	}
}

func imageCount(n *int) int {
	if n == nil || *n == 0 {
		return 1
	}
	return *n
}

func quiverRawOptions(providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return nil
	}
	m, _ := providerOptions["quiverai"].(map[string]interface{})
	return m
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
		SVG                string `json:"svg"`
		MimeType           string `json:"mime_type"`
		LoopPeriodMs       *int   `json:"loop_period_ms,omitempty"`
		OpeningAnimationMs *int   `json:"opening_animation_ms,omitempty"`
	} `json:"data"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Credits *int `json:"credits,omitempty"`
}

type quiverAPIError struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

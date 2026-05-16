package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingPart is implemented by TextEmbeddingPart and ImageEmbeddingPart.
// The unexported marker method seals the interface within this package.
type EmbeddingPart interface {
	embeddingPart()
}

// TextEmbeddingPart is a text part for multimodal embedding.
type TextEmbeddingPart struct {
	Text string
}

func (TextEmbeddingPart) embeddingPart() {}

// ImageEmbeddingPart is an image part for multimodal embedding.
type ImageEmbeddingPart struct {
	// MimeType is the MIME type of the image (e.g., "image/jpeg").
	MimeType string
	// Data is the raw image bytes.
	Data []byte
}

func (ImageEmbeddingPart) embeddingPart() {}

type rawInlineDataEmbeddingPart struct {
	MimeType string
	Data     string
}

func (rawInlineDataEmbeddingPart) embeddingPart() {}

// FileDataEmbeddingPart is a provider-reference file part for multimodal embedding.
// It maps to the Google API's `fileData` shape.
type FileDataEmbeddingPart struct {
	// MimeType is the MIME type of the file (e.g. "application/pdf", "image/png").
	MimeType string
	// FileURI is the provider reference URI returned by the Google Files API.
	FileURI string
}

func (FileDataEmbeddingPart) embeddingPart() {}

// GoogleEmbeddingProviderOptions contains Google-specific options for embedding.
type GoogleEmbeddingProviderOptions struct {
	// Parts provides additional multimodal content parts alongside the text input.
	// Each element corresponds to a single embedding value and is appended to that
	// value's content.parts in the API request.
	Parts []EmbeddingPart

	// Content provides TypeScript-compatible per-value multimodal content. Each
	// entry corresponds to the embedding value at the same index. A nil entry is
	// text-only; a non-nil entry must contain at least one part.
	Content [][]EmbeddingPart

	// OutputDimensionality optionally truncates the embedding vector length.
	OutputDimensionality *int

	// TaskType maps to the Google embedding taskType request field.
	TaskType string

	outputDimensionality interface{}
}

// EmbeddingModel implements the provider.EmbeddingModel interface for Google
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Google embedding model
func NewEmbeddingModel(provider *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return m.provider.Name()
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call.
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 2048
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	googleOpts, err := parseEmbeddingProviderOptions(opts)
	if err != nil {
		return nil, err
	}
	contentParts, err := embeddingPartsForValue(input, 0, 1, googleOpts)
	if err != nil {
		return nil, err
	}
	reqBody := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", m.modelID),
		"content": map[string]interface{}{
			"parts": contentParts,
		},
	}
	addEmbeddingModelOptions(reqBody, googleOpts)
	path := fmt.Sprintf("/models/%s:embedContent", m.modelID)

	var response googleEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if response.Embedding == nil || len(response.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return &types.EmbeddingResult{
		Embedding: response.Embedding.Values,
		Usage: types.EmbeddingUsage{
			// Google doesn't return token counts for embeddings
			InputTokens: 0,
			TotalTokens: 0,
		},
		Response: types.EmbeddingResponse{Headers: map[string][]string(httpResp.Headers), Body: json.RawMessage(httpResp.Body)},
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) == 0 {
		return &types.EmbeddingsResult{
			Embeddings: [][]float64{},
			Usage:      types.EmbeddingUsage{},
		}, nil
	}
	googleOpts, err := parseEmbeddingProviderOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(inputs) > m.MaxEmbeddingsPerCall() {
		return nil, providererrors.NewProviderError(
			m.Provider(),
			0,
			"too_many_embedding_values_for_call",
			fmt.Sprintf("too many embedding values for model %s: got %d, max %d", m.modelID, len(inputs), m.MaxEmbeddingsPerCall()),
			nil,
		)
	}
	if len(inputs) == 1 {
		one, err := m.DoEmbed(ctx, inputs[0], opts)
		if err != nil {
			return nil, err
		}
		return &types.EmbeddingsResult{
			Embeddings: [][]float64{one.Embedding},
			Usage:      one.Usage,
			Warnings:   one.Warnings,
			Responses:  []types.EmbeddingResponse{one.Response},
		}, nil
	}

	requests := make([]map[string]interface{}, 0, len(inputs))
	for i, input := range inputs {
		parts, err := embeddingPartsForValue(input, i, len(inputs), googleOpts)
		if err != nil {
			return nil, err
		}
		request := map[string]interface{}{
			"model": fmt.Sprintf("models/%s", m.modelID),
			"content": map[string]interface{}{
				"role":  "user",
				"parts": parts,
			},
		}
		addEmbeddingModelOptions(request, googleOpts)
		requests = append(requests, request)
	}

	reqBody := map[string]interface{}{"requests": requests}
	path := fmt.Sprintf("/models/%s:batchEmbedContents", m.modelID)

	var response googleBatchEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	if len(response.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("google embedding response count %d does not match input count %d", len(response.Embeddings), len(inputs))
	}
	embeddings := make([][]float64, len(response.Embeddings))
	for i, item := range response.Embeddings {
		if len(item.Values) == 0 {
			return nil, fmt.Errorf("no embedding data in response for input %d", i)
		}
		embeddings[i] = item.Values
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: 0,
			TotalTokens: 0,
		},
		Responses: []types.EmbeddingResponse{{Headers: map[string][]string(httpResp.Headers), Body: json.RawMessage(httpResp.Body)}},
	}, nil
}

// DoEmbedParts performs embedding for a single input with optional multimodal parts.
// The text parameter provides the primary text content; parts provides additional
// content (e.g. an image) to embed alongside it.
func (m *EmbeddingModel) DoEmbedParts(ctx context.Context, text string, parts []EmbeddingPart) (*types.EmbeddingResult, error) {
	apiParts, err := buildEmbeddingAPIParts(text, parts)
	if err != nil {
		return nil, err
	}

	reqBody := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", m.modelID),
		"content": map[string]interface{}{
			"parts": apiParts,
		},
	}
	path := fmt.Sprintf("/models/%s:embedContent", m.modelID)

	var response googleEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if response.Embedding == nil || len(response.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return &types.EmbeddingResult{
		Embedding: response.Embedding.Values,
		Usage:     types.EmbeddingUsage{},
		Response:  types.EmbeddingResponse{Headers: map[string][]string(httpResp.Headers), Body: json.RawMessage(httpResp.Body)},
	}, nil
}

// buildEmbeddingAPIParts converts a text string plus optional EmbeddingParts to the
// list of content.parts expected by the Google embedContent API.
func buildEmbeddingAPIParts(text string, parts []EmbeddingPart) ([]map[string]interface{}, error) {
	apiParts := []map[string]interface{}{
		{"text": text},
	}
	extra, err := embeddingAPIParts(parts)
	if err != nil {
		return nil, err
	}
	apiParts = append(apiParts, extra...)
	return apiParts, nil
}

func embeddingAPIParts(parts []EmbeddingPart) ([]map[string]interface{}, error) {
	apiParts := make([]map[string]interface{}, 0, len(parts))
	for _, p := range parts {
		switch v := p.(type) {
		case TextEmbeddingPart:
			apiParts = append(apiParts, map[string]interface{}{"text": v.Text})
		case ImageEmbeddingPart:
			if v.MimeType == "" {
				return nil, fmt.Errorf("image embedding part mime type cannot be empty")
			}
			if len(v.Data) == 0 {
				return nil, fmt.Errorf("image embedding part data cannot be empty")
			}
			apiParts = append(apiParts, map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": v.MimeType,
					"data":     base64.StdEncoding.EncodeToString(v.Data),
				},
			})
		case rawInlineDataEmbeddingPart:
			if v.MimeType == "" {
				return nil, fmt.Errorf("inlineData embedding part mime type cannot be empty")
			}
			if v.Data == "" {
				return nil, fmt.Errorf("inlineData embedding part data cannot be empty")
			}
			apiParts = append(apiParts, map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": v.MimeType,
					"data":     v.Data,
				},
			})
		case FileDataEmbeddingPart:
			if v.MimeType == "" {
				return nil, fmt.Errorf("fileData embedding part mime type cannot be empty")
			}
			if v.FileURI == "" {
				return nil, fmt.Errorf("fileData embedding part file URI cannot be empty")
			}
			apiParts = append(apiParts, map[string]interface{}{
				"fileData": map[string]interface{}{
					"mimeType": v.MimeType,
					"fileUri":  v.FileURI,
				},
			})
		}
	}
	return apiParts, nil
}

func embeddingPartsForValue(text string, index, valueCount int, opts GoogleEmbeddingProviderOptions) ([]map[string]interface{}, error) {
	if opts.Content != nil {
		if len(opts.Content) != valueCount {
			return nil, fmt.Errorf("the number of multimodal content entries (%d) must match the number of values (%d)", len(opts.Content), valueCount)
		}
		textParts := make([]map[string]interface{}, 0, 1+len(opts.Content[index]))
		if text != "" {
			textParts = append(textParts, map[string]interface{}{"text": text})
		}
		if opts.Content[index] == nil {
			if len(textParts) == 0 {
				textParts = append(textParts, map[string]interface{}{"text": text})
			}
			return textParts, nil
		}
		if len(opts.Content[index]) == 0 {
			return nil, fmt.Errorf("google embedding content entry %d must contain at least one part or be nil", index)
		}
		extra, err := embeddingAPIParts(opts.Content[index])
		if err != nil {
			return nil, err
		}
		return append(textParts, extra...), nil
	}
	return buildEmbeddingAPIParts(text, opts.Parts)
}

func addEmbeddingModelOptions(body map[string]interface{}, opts GoogleEmbeddingProviderOptions) {
	if opts.outputDimensionality != nil {
		body["outputDimensionality"] = opts.outputDimensionality
	} else if opts.OutputDimensionality != nil {
		body["outputDimensionality"] = *opts.OutputDimensionality
	}
	if opts.TaskType != "" {
		body["taskType"] = opts.TaskType
	}
}

func parseEmbeddingProviderOptions(opts *provider.EmbedModelOptions) (GoogleEmbeddingProviderOptions, error) {
	if opts == nil || opts.ProviderOptions == nil {
		return GoogleEmbeddingProviderOptions{}, nil
	}
	raw, ok := opts.ProviderOptions["google"]
	if !ok || raw == nil {
		return GoogleEmbeddingProviderOptions{}, nil
	}
	switch v := raw.(type) {
	case GoogleEmbeddingProviderOptions:
		if err := validateEmbeddingProviderOptions(v); err != nil {
			return GoogleEmbeddingProviderOptions{}, err
		}
		if v.OutputDimensionality != nil {
			v.outputDimensionality = *v.OutputDimensionality
		}
		return v, nil
	case *GoogleEmbeddingProviderOptions:
		if v == nil {
			return GoogleEmbeddingProviderOptions{}, nil
		}
		if err := validateEmbeddingProviderOptions(*v); err != nil {
			return GoogleEmbeddingProviderOptions{}, err
		}
		out := *v
		if out.OutputDimensionality != nil {
			out.outputDimensionality = *out.OutputDimensionality
		}
		return out, nil
	case map[string]interface{}:
		return parseEmbeddingOptionsMap(v)
	default:
		return GoogleEmbeddingProviderOptions{}, fmt.Errorf("google embedding provider options must be GoogleEmbeddingProviderOptions or map[string]interface{}, got %T", raw)
	}
}

func parseEmbeddingOptionsMap(raw map[string]interface{}) (GoogleEmbeddingProviderOptions, error) {
	var opts GoogleEmbeddingProviderOptions
	if v, ok := raw["outputDimensionality"]; ok {
		switch n := v.(type) {
		case int:
			opts.OutputDimensionality = &n
			opts.outputDimensionality = n
		case int64:
			i := int(n)
			opts.OutputDimensionality = &i
			opts.outputDimensionality = n
		case float64:
			opts.outputDimensionality = n
		default:
			return opts, fmt.Errorf("google embedding outputDimensionality must be a number")
		}
	}
	if v, ok := raw["taskType"]; ok {
		task, ok := v.(string)
		if !ok {
			return opts, fmt.Errorf("google embedding taskType must be a string")
		}
		if !validGoogleEmbeddingTaskType(task) {
			return opts, fmt.Errorf("google embedding taskType %q is not supported", task)
		}
		opts.TaskType = task
	}
	if v, ok := raw["content"]; ok {
		content, err := parseEmbeddingContent(v)
		if err != nil {
			return opts, err
		}
		opts.Content = content
	}
	return opts, nil
}

func validateEmbeddingProviderOptions(opts GoogleEmbeddingProviderOptions) error {
	if opts.TaskType != "" && !validGoogleEmbeddingTaskType(opts.TaskType) {
		return fmt.Errorf("google embedding taskType %q is not supported", opts.TaskType)
	}
	for i, parts := range opts.Content {
		if parts != nil && len(parts) == 0 {
			return fmt.Errorf("google embedding content entry %d must contain at least one part or be nil", i)
		}
	}
	return nil
}

func validGoogleEmbeddingTaskType(task string) bool {
	switch task {
	case "SEMANTIC_SIMILARITY", "CLASSIFICATION", "CLUSTERING", "RETRIEVAL_DOCUMENT", "RETRIEVAL_QUERY", "QUESTION_ANSWERING", "FACT_VERIFICATION", "CODE_RETRIEVAL_QUERY":
		return true
	default:
		return false
	}
}

func parseEmbeddingContent(raw interface{}) ([][]EmbeddingPart, error) {
	values, ok := raw.([]interface{})
	if !ok {
		if typed, ok := raw.([][]EmbeddingPart); ok {
			return typed, nil
		}
		return nil, fmt.Errorf("google embedding content must be an array")
	}
	out := make([][]EmbeddingPart, len(values))
	for i, entry := range values {
		if entry == nil {
			continue
		}
		items, ok := entry.([]interface{})
		if !ok {
			return nil, fmt.Errorf("google embedding content entry %d must be an array or nil", i)
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("google embedding content entry %d must contain at least one part or be nil", i)
		}
		parts := make([]EmbeddingPart, 0, len(items))
		for _, item := range items {
			part, err := parseEmbeddingContentPart(item)
			if err != nil {
				return nil, fmt.Errorf("google embedding content entry %d: %w", i, err)
			}
			parts = append(parts, part)
		}
		out[i] = parts
	}
	return out, nil
}

func parseEmbeddingContentPart(raw interface{}) (EmbeddingPart, error) {
	switch v := raw.(type) {
	case TextEmbeddingPart, ImageEmbeddingPart, FileDataEmbeddingPart:
		return v.(EmbeddingPart), nil
	case map[string]interface{}:
		if text, ok := v["text"]; ok {
			s, ok := text.(string)
			if !ok {
				return nil, fmt.Errorf("text part text must be a string")
			}
			return TextEmbeddingPart{Text: s}, nil
		}
		if inline, ok := v["inlineData"].(map[string]interface{}); ok {
			mime, _ := inline["mimeType"].(string)
			data, _ := inline["data"].(string)
			if mime == "" || data == "" {
				return nil, fmt.Errorf("inlineData part requires mimeType and data")
			}
			return rawInlineDataEmbeddingPart{MimeType: mime, Data: data}, nil
		}
		if fileData, ok := v["fileData"].(map[string]interface{}); ok {
			mime, _ := fileData["mimeType"].(string)
			uri, _ := fileData["fileUri"].(string)
			if mime == "" || uri == "" {
				return nil, fmt.Errorf("fileData part requires mimeType and fileUri")
			}
			return FileDataEmbeddingPart{MimeType: mime, FileURI: uri}, nil
		}
	}
	return nil, fmt.Errorf("unsupported embedding content part %T", raw)
}

// handleError converts various errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return providererrors.NewProviderError("google", 0, "", err.Error(), err)
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

// googleEmbeddingResponse represents the Google embeddings API response
type googleEmbeddingResponse struct {
	Embedding *struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}

type googleBatchEmbeddingResponse struct {
	Embeddings []struct {
		Values []float64 `json:"values"`
	} `json:"embeddings"`
}

package perplexity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for Perplexity
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Perplexity language model
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "perplexity"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return false
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return false
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	warnings := m.checkWarnings(opts)
	reqBody, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, err
	}
	var response perplexityResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/chat/completions",
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response, resp.Body)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	result.ResponseMetadata = &types.ResponseMetadata{
		ID:        response.ID,
		Timestamp: time.Unix(response.Created, 0),
		ModelID:   response.Model,
		Headers:   result.ResponseHeaders,
	}
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	warnings := m.checkWarnings(opts)
	reqBody, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, err
	}
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/chat/completions",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	inner := newPerplexityStream(httpResp.Body, opts.IncludeRawChunks, providerutils.ExtractHeaders(httpResp.Header))
	return newPerplexityStartStream(inner, warnings), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
	}
	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.TopK != nil {
		body["top_k"] = *opts.TopK
	}
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" {
		jsonSchema := map[string]interface{}{}
		if opts.ResponseFormat.Schema != nil {
			jsonSchema["schema"] = opts.ResponseFormat.Schema
		}
		body["response_format"] = map[string]interface{}{
			"type":        "json_schema",
			"json_schema": jsonSchema,
		}
	}
	if raw, ok := opts.ProviderOptions["perplexity"]; ok && raw != nil {
		rawMap, ok := raw.(map[string]interface{})
		if !ok {
			return nil, invalidPerplexityProviderOptions("perplexity", fmt.Sprintf("must be an object, got %T", raw))
		}
		parsed, extras, err := parsePerplexityLanguageModelOptions(rawMap)
		if err != nil {
			return nil, err
		}
		for k, v := range parsed.toWireMap() {
			body[k] = v
		}
		for k, v := range extras {
			body[k] = v
		}
	}
	var messages []map[string]interface{}
	var err error
	if opts.Prompt.IsMessages() {
		messages, err = convertToPerplexityMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		messages, err = convertToPerplexityMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}
	if err != nil {
		return nil, err
	}
	if opts.Prompt.System != "" {
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": opts.Prompt.System,
		}
		messages = append([]map[string]interface{}{systemMsg}, messages...)
	}
	if len(messages) > 0 {
		body["messages"] = messages
	}
	return body, nil
}

func (m *LanguageModel) checkWarnings(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "stopSequences"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoning",
			Details: "This provider does not support reasoning configuration.",
		})
	}
	return warnings
}

func convertToPerplexityMessages(messages []types.Message) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			result = append(result, map[string]interface{}{
				"role":    "system",
				"content": contentText(msg.Content),
			})
		case types.RoleUser, types.RoleAssistant:
			content, err := convertPerplexityContent(msg.Content)
			if err != nil {
				return nil, err
			}
			result = append(result, map[string]interface{}{
				"role":    string(msg.Role),
				"content": content,
			})
		case types.RoleTool:
			return nil, fmt.Errorf("perplexity: unsupported functionality: tool messages")
		default:
			return nil, fmt.Errorf("perplexity: unsupported role %q", msg.Role)
		}
	}
	return result, nil
}

func convertPerplexityContent(parts []types.ContentPart) (interface{}, error) {
	hasMultipart := false
	for _, part := range parts {
		switch p := part.(type) {
		case types.ImageContent:
			hasMultipart = true
		case types.FileContent:
			mediaType := firstNonEmpty(p.MediaType, p.MimeType, p.FileData.MediaType)
			if topLevelMediaType(mediaType) == "image" || topLevelMediaType(mediaType) == "application" {
				hasMultipart = true
			}
		}
	}

	contentParts := make([]map[string]interface{}, 0, len(parts))
	for i, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			contentParts = append(contentParts, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ImageContent:
			url := p.URL
			mediaType := p.MimeType
			if mediaType == "" {
				mediaType = "image/png"
			}
			if url == "" {
				url = fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(p.Image))
			}
			contentParts = append(contentParts, map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}})
		case types.FileContent:
			converted, err := convertPerplexityFilePart(p, i)
			if err != nil {
				return nil, err
			}
			if converted != nil {
				contentParts = append(contentParts, converted)
			}
		}
	}

	if hasMultipart {
		return contentParts, nil
	}
	var b strings.Builder
	for _, part := range contentParts {
		if part["type"] == "text" {
			if text, ok := part["text"].(string); ok {
				b.WriteString(text)
			}
		}
	}
	return b.String(), nil
}

func convertPerplexityFilePart(file types.FileContent, index int) (map[string]interface{}, error) {
	normalized, err := prompt.NormalizeFileContent(file)
	if err != nil {
		return nil, err
	}
	mediaType := firstNonEmpty(normalized.MediaType, normalized.MimeType, normalized.FileData.MediaType)
	switch normalized.FileData.Type {
	case types.FileDataTypeReference:
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "file parts with provider references"}
	case types.FileDataTypeText:
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
	case types.FileDataTypeURL, types.FileDataTypeData:
		isDataPart := normalized.FileData.Type == types.FileDataTypeData
		top := topLevelMediaType(mediaType)
		switch top {
		case "application":
			fullMediaType, resolveErr := resolvePerplexityMediaType(mediaType, normalized.Data, isDataPart)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if fullMediaType != "application/pdf" {
				return nil, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("file part media type %s", fullMediaType)}
			}
			url := normalized.URL
			if isDataPart {
				url = base64.StdEncoding.EncodeToString(normalized.Data)
			}
			filename := normalized.Filename
			if filename == "" {
				filename = fmt.Sprintf("document-%d.pdf", index)
			}
			return map[string]interface{}{
				"type":      "file_url",
				"file_url":  map[string]interface{}{"url": url},
				"file_name": filename,
			}, nil
		case "image":
			url := normalized.URL
			if isDataPart {
				fullMediaType, resolveErr := resolvePerplexityMediaType(mediaType, normalized.Data, isDataPart)
				if resolveErr != nil {
					return nil, resolveErr
				}
				url = fmt.Sprintf("data:%s;base64,%s", fullMediaType, base64.StdEncoding.EncodeToString(normalized.Data))
			}
			return map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": url},
			}, nil
		default:
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("file part media type %s", mediaType)}
		}
	}
	return nil, nil
}

// isFullMediaType reports whether mediaType is already a complete "type/subtype"
// media type (i.e. has a non-empty, non-wildcard subtype), mirroring the TS
// SDK's isFullMediaType helper.
func isFullMediaType(mediaType string) bool {
	idx := strings.Index(mediaType, "/")
	if idx == -1 {
		return false
	}
	subtype := mediaType[idx+1:]
	return subtype != "" && subtype != "*"
}

// resolvePerplexityMediaType resolves a file part's media type to a full
// "type/subtype" form. If mediaType is already a full media type it is
// returned as-is. Otherwise, when inline bytes are available, the subtype is
// sniffed from the bytes via signature detection. If neither applies, an
// UnsupportedFunctionalityError is returned — mirrors the TS SDK's
// resolveFullMediaType.
func resolvePerplexityMediaType(mediaType string, data []byte, isDataPart bool) (string, error) {
	if isFullMediaType(mediaType) {
		return mediaType, nil
	}
	if isDataPart {
		if detected, ok := fileutil.DetectMediaTypeSignature(data, topLevelMediaType(mediaType)); ok {
			return detected, nil
		}
		return "", &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("file of media type %q must specify subtype since it could not be auto-detected", mediaType),
		}
	}
	return "", &providererrors.UnsupportedFunctionalityError{
		Functionality: fmt.Sprintf("file of media type %q must specify subtype since it is not passed as inline bytes", mediaType),
	}
}

func contentText(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if text, ok := part.(types.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func topLevelMediaType(mediaType string) string {
	if idx := strings.Index(mediaType, "/"); idx >= 0 {
		return mediaType[:idx]
	}
	return mediaType
}

// PerplexityImage mirrors the image object returned by the Perplexity API.
// Matches TS: { imageUrl, originUrl, height, width }.
type PerplexityImage struct {
	ImageUrl  string `json:"imageUrl"`
	OriginUrl string `json:"originUrl"`
	Height    int    `json:"height"`
	Width     int    `json:"width"`
}

// PerplexityUsageMeta contains Perplexity-specific usage counters that are not
// part of the standard token usage. Matches TS providerMetadata.perplexity.usage.
type PerplexityUsageMeta struct {
	CitationTokens   *int `json:"citationTokens"`
	NumSearchQueries *int `json:"numSearchQueries"`
}

// PerplexityCost contains the per-request cost breakdown returned by Perplexity.
// All fields use *float64 so that missing fields are represented as null rather
// than zero — matching the TS SDK's null semantics.
type PerplexityCost struct {
	InputTokensCost  *float64 `json:"inputTokensCost"`
	OutputTokensCost *float64 `json:"outputTokensCost"`
	RequestCost      *float64 `json:"requestCost"`
	TotalCost        *float64 `json:"totalCost"`
}

// PerplexityMetadata is the full providerMetadata.perplexity object.
// It is always present on non-streaming results, matching the TS SDK which
// unconditionally sets providerMetadata.perplexity.
// Cost is nil when the API does not return cost information.
type PerplexityMetadata struct {
	Images []PerplexityImage   `json:"images"`
	Usage  PerplexityUsageMeta `json:"usage"`
	Cost   *PerplexityCost     `json:"cost"`
}

func (m *LanguageModel) convertResponse(response perplexityResponse, rawBody []byte) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
		}
	}
	choice := response.Choices[0]
	usage := convertPerplexityUsagePtr(response.Usage)
	usage.Raw = extractRawUsage(rawBody)
	result := &types.GenerateResult{
		Text:         choice.Message.Content,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        usage,
		RawResponse:  response,
	}
	if len(response.Citations) > 0 {
		result.Content = make([]types.ContentPart, 0, len(response.Citations))
		for i, u := range response.Citations {
			result.Content = append(result.Content, types.SourceContent{
				SourceType: "url",
				ID:         fmt.Sprintf("perplexity-citation-%d", i),
				URL:        u,
			})
		}
	}

	// Build providerMetadata.perplexity — always set (matches TS SDK behaviour).
	meta := PerplexityMetadata{
		Usage: PerplexityUsageMeta{
			CitationTokens:   nil,
			NumSearchQueries: nil,
		},
	}
	if response.Usage != nil {
		meta.Usage.CitationTokens = response.Usage.CitationTokens
		meta.Usage.NumSearchQueries = response.Usage.NumSearchQueries
	}

	// Map images from API wire format to public type.
	if len(response.Images) > 0 {
		meta.Images = make([]PerplexityImage, len(response.Images))
		for i, img := range response.Images {
			meta.Images[i] = PerplexityImage(img)
		}
	}

	// Cost is a nested object in the API response (usage.cost.*).
	if response.Usage != nil && response.Usage.Cost != nil {
		c := response.Usage.Cost
		meta.Cost = &PerplexityCost{
			InputTokensCost:  c.InputTokensCost,
			OutputTokensCost: c.OutputTokensCost,
			RequestCost:      c.RequestCost,
			TotalCost:        c.TotalCost,
		}
	}

	result.ProviderMetadata = map[string]interface{}{
		"perplexity": meta,
	}

	return result
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("perplexity", 0, "", err.Error(), err)
}

func convertPerplexityUsagePtr(usage *perplexityUsage) types.Usage {
	if usage == nil {
		return types.Usage{}
	}
	return convertPerplexityUsage(*usage)
}

func convertPerplexityUsage(usage perplexityUsage) types.Usage {
	p, c, t := int64(usage.PromptTokens), int64(usage.CompletionTokens), int64(usage.TotalTokens)
	result := types.Usage{InputTokens: &p, OutputTokens: &c, TotalTokens: &t}
	var cached int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cached = int64(*usage.PromptTokensDetails.CachedTokens)
	}
	var textTokens *int64
	var imageTokens *int64
	if usage.PromptTokensDetails != nil {
		if usage.PromptTokensDetails.TextTokens != nil {
			textVal := int64(*usage.PromptTokensDetails.TextTokens)
			textTokens = &textVal
		}
		if usage.PromptTokensDetails.ImageTokens != nil {
			imageVal := int64(*usage.PromptTokensDetails.ImageTokens)
			imageTokens = &imageVal
		}
	}
	var reasoning int64
	if usage.ReasoningTokens != nil {
		reasoning = int64(*usage.ReasoningTokens)
	}
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}
	noCache := p - cached
	result.InputDetails = &types.InputTokenDetails{NoCacheTokens: &noCache, CacheReadTokens: nil, CacheWriteTokens: nil, TextTokens: textTokens, ImageTokens: imageTokens}
	if cached > 0 {
		result.InputDetails.CacheReadTokens = &cached
	}
	text := c - reasoning
	result.OutputDetails = &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning}
	// Raw is populated by the caller (extractRawUsage) from the original wire
	// bytes so that ALL fields returned by the API are preserved — including
	// any not modeled by the perplexityUsage struct — matching the TS SDK's
	// `raw: usage` (the untransformed response object).
	return result
}

// extractRawUsage decodes the top-level "usage" object from the raw response
// body into a generic map, preserving every field the API returned (including
// ones not modeled by perplexityUsage). Returns nil if the body has no usage
// object or cannot be parsed.
func extractRawUsage(body []byte) map[string]interface{} {
	if len(body) == 0 {
		return nil
	}
	var wrapper struct {
		Usage map[string]interface{} `json:"usage"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil
	}
	return wrapper.Usage
}

type perplexityResponse struct {
	ID        string               `json:"id"`
	Created   int64                `json:"created"`
	Model     string               `json:"model"`
	Citations []string             `json:"citations,omitempty"`
	Images    []perplexityRawImage `json:"images,omitempty"`
	Choices   []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *perplexityUsage `json:"usage"`
}

// perplexityRawImage is the wire format of an image returned by the Perplexity API.
type perplexityRawImage struct {
	ImageUrl  string `json:"image_url"`
	OriginUrl string `json:"origin_url"`
	Height    int    `json:"height"`
	Width     int    `json:"width"`
}

// perplexityCostRaw is the wire format of the nested cost object in usage.
type perplexityCostRaw struct {
	InputTokensCost  *float64 `json:"input_tokens_cost,omitempty"`
	OutputTokensCost *float64 `json:"output_tokens_cost,omitempty"`
	RequestCost      *float64 `json:"request_cost,omitempty"`
	TotalCost        *float64 `json:"total_cost,omitempty"`
}

type perplexityUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// Perplexity-specific usage counters.
	CitationTokens   *int `json:"citation_tokens,omitempty"`
	NumSearchQueries *int `json:"num_search_queries,omitempty"`
	// Cost is a nested object in the API response (not flat fields).
	Cost                *perplexityCostRaw `json:"cost,omitempty"`
	ReasoningTokens     *int               `json:"reasoning_tokens,omitempty"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
		AudioTokens  *int `json:"audio_tokens,omitempty"`
		TextTokens   *int `json:"text_tokens,omitempty"`
		ImageTokens  *int `json:"image_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type perplexityStream struct {
	*streaming.OpenAICompatStream
	usage                   *perplexityUsage
	usageRaw                map[string]interface{}
	providerMetadata        PerplexityMetadata
	responseHeaders         map[string]string
	emittedCitations        bool
	emittedResponseMetadata bool
	metadataMarshalErr      error
}

func newPerplexityStream(reader io.ReadCloser, includeRawChunks bool, responseHeaders map[string]string) *perplexityStream {
	s := &perplexityStream{
		OpenAICompatStream: streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason),
		responseHeaders:    responseHeaders,
	}
	s.IncludeRawChunks = includeRawChunks
	s.OnBeforeDelta = func(data []byte) []*provider.StreamChunk {
		var peek struct {
			Citations []string             `json:"citations"`
			Images    []perplexityRawImage `json:"images"`
			Usage     *perplexityUsage     `json:"usage"`
			ID        string               `json:"id"`
			Created   int64                `json:"created"`
			Model     string               `json:"model"`
		}
		if err := json.Unmarshal(data, &peek); err != nil {
			return nil
		}

		if peek.Usage != nil {
			s.usage = peek.Usage
			s.usageRaw = extractRawUsage(data)
			s.providerMetadata.Usage = PerplexityUsageMeta{
				CitationTokens:   peek.Usage.CitationTokens,
				NumSearchQueries: peek.Usage.NumSearchQueries,
			}
			if c := peek.Usage.Cost; c != nil {
				s.providerMetadata.Cost = &PerplexityCost{
					InputTokensCost:  c.InputTokensCost,
					OutputTokensCost: c.OutputTokensCost,
					RequestCost:      c.RequestCost,
					TotalCost:        c.TotalCost,
				}
			} else {
				s.providerMetadata.Cost = nil
			}
		}

		if len(peek.Images) > 0 {
			s.providerMetadata.Images = make([]PerplexityImage, len(peek.Images))
			for i, img := range peek.Images {
				s.providerMetadata.Images[i] = PerplexityImage(img)
			}
		}

		var chunks []*provider.StreamChunk
		if !s.emittedResponseMetadata {
			s.emittedResponseMetadata = true
			chunks = append(chunks, &provider.StreamChunk{
				Type: provider.ChunkTypeResponseMetadata,
				ResponseMetadata: &provider.ResponseMetadata{
					ID:        peek.ID,
					Timestamp: time.Unix(peek.Created, 0),
					ModelID:   peek.Model,
					Headers:   s.responseHeaders,
				},
			})
		}
		if !s.emittedCitations && len(peek.Citations) > 0 {
			s.emittedCitations = true
			for i, u := range peek.Citations {
				chunks = append(chunks, &provider.StreamChunk{
					Type: provider.ChunkTypeSource,
					SourceContent: &types.SourceContent{
						SourceType: "url",
						ID:         fmt.Sprintf("perplexity-citation-%d", i),
						URL:        u,
					},
				})
			}
		}
		return chunks
	}
	return s
}

func (s *perplexityStream) Next() (*provider.StreamChunk, error) {
	chunk, err := s.OpenAICompatStream.Next()
	if chunk != nil && chunk.Type == provider.ChunkTypeFinish {
		if s.usage != nil {
			usage := convertPerplexityUsage(*s.usage)
			usage.Raw = s.usageRaw
			chunk.Usage = &usage
		}
		if s.metadataMarshalErr == nil {
			raw, marshalErr := json.Marshal(map[string]interface{}{
				"perplexity": s.providerMetadata,
			})
			if marshalErr != nil {
				s.metadataMarshalErr = marshalErr
			} else {
				chunk.ProviderMetadata = raw
			}
		}
	}
	return chunk, err
}

func (s *perplexityStream) Err() error {
	if s.metadataMarshalErr != nil {
		return s.metadataMarshalErr
	}
	return s.OpenAICompatStream.Err()
}

type perplexityStartStream struct {
	inner    provider.TextStream
	warnings []types.Warning
	started  bool
}

func newPerplexityStartStream(inner provider.TextStream, warnings []types.Warning) provider.TextStream {
	return &perplexityStartStream{inner: inner, warnings: warnings}
}

func (s *perplexityStartStream) Next() (*provider.StreamChunk, error) {
	if !s.started {
		s.started = true
		return &provider.StreamChunk{
			Type:     provider.ChunkTypeStreamStart,
			Warnings: s.warnings,
		}, nil
	}
	return s.inner.Next()
}

func (s *perplexityStartStream) Err() error   { return s.inner.Err() }
func (s *perplexityStartStream) Close() error { return s.inner.Close() }

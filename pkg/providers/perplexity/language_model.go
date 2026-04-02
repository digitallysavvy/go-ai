package perplexity

import (
	"context"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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
	var warnings []types.Warning
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "Perplexity does not support reasoning",
		})
	}
	reqBody := m.buildRequestBody(opts, false)
	var response perplexityResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/chat/completions",
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	var warnings []types.Warning
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "Perplexity does not support reasoning",
		})
	}
	reqBody := m.buildRequestBody(opts, true)
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
	inner := newPerplexityStream(httpResp.Body)
	return providerutils.WithResponseMetadata(streaming.NewWarningsStream(inner, warnings), httpResp.Header), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": opts.Prompt.System,
		}
		body["messages"] = append([]map[string]interface{}{systemMsg}, messages...)
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
	return body
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

func (m *LanguageModel) convertResponse(response perplexityResponse) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
		}
	}
	choice := response.Choices[0]
	result := &types.GenerateResult{
		Text:         choice.Message.Content,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        convertPerplexityUsage(response.Usage),
		RawResponse:  response,
	}

	// Build providerMetadata.perplexity — always set (matches TS SDK behaviour).
	meta := PerplexityMetadata{
		Usage: PerplexityUsageMeta{
			CitationTokens:   response.Usage.CitationTokens,
			NumSearchQueries: response.Usage.NumSearchQueries,
		},
	}

	// Map images from API wire format to public type.
	if len(response.Images) > 0 {
		meta.Images = make([]PerplexityImage, len(response.Images))
		for i, img := range response.Images {
			meta.Images[i] = PerplexityImage(img)
		}
	}

	// Cost is a nested object in the API response (usage.cost.*).
	if c := response.Usage.Cost; c != nil {
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
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}
	if cached > 0 || textTokens != nil || imageTokens != nil {
		noCache := p - cached
		result.InputDetails = &types.InputTokenDetails{NoCacheTokens: &noCache, CacheReadTokens: &cached, CacheWriteTokens: nil, TextTokens: textTokens, ImageTokens: imageTokens}
	}
	if reasoning > 0 {
		text := c - reasoning
		result.OutputDetails = &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning}
	}
	result.Raw = map[string]interface{}{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens}
	if usage.PromptTokensDetails != nil {
		result.Raw["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokensDetails != nil {
		result.Raw["completion_tokens_details"] = usage.CompletionTokensDetails
	}
	return result
}


type perplexityResponse struct {
	ID        string               `json:"id"`
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
	Usage perplexityUsage `json:"usage"`
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
	Cost *perplexityCostRaw `json:"cost,omitempty"`
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
}

func newPerplexityStream(reader io.ReadCloser) *perplexityStream {
	return &perplexityStream{
		OpenAICompatStream: streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason),
	}
}

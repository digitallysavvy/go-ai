package xai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements the provider.LanguageModel interface for xAI
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new xAI language model
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
	return "xai"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	warnings := m.checkUnsupportedOptions(opts)
	reqBody := m.buildRequestBody(opts, false)
	var response xaiResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	// Surface API-level errors returned in the response body.
	if response.Error != nil {
		return nil, m.handleError(fmt.Errorf("%s", *response.Error))
	}
	result := m.convertResponse(response, lastAssistantText(opts))
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	return result, nil
}

// checkUnsupportedOptions returns warnings for options xAI chat API does not support.
func (m *LanguageModel) checkUnsupportedOptions(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "XAI does not support topK",
		})
	}
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "XAI does not support frequencyPenalty",
		})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "XAI does not support presencePenalty",
		})
	}
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "XAI does not support stopSequences",
		})
	}
	return warnings
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody := m.buildRequestBody(opts, true)
	// Request usage data in the final streaming chunk (matches TypeScript SDK behavior).
	reqBody["stream_options"] = map[string]interface{}{"include_usage": true}
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	return providerutils.WithResponseMetadata(newXAIStream(httpResp.Body, lastAssistantText(opts)), httpResp.Header), nil
}

// XAIChatProviderOptions contains XAI-specific options for the chat completions path.
type XAIChatProviderOptions struct {
	// Logprobs enables log probability output for generated tokens.
	Logprobs *bool `json:"logprobs,omitempty"`

	// TopLogprobs is the number of most likely tokens to return per position.
	// Setting this implicitly enables Logprobs.
	TopLogprobs *int `json:"topLogprobs,omitempty"`

	// ReasoningEffort controls the reasoning depth for Grok reasoning models.
	// Valid values: "low", "high". Overrides the top-level opts.Reasoning.
	// Note: Chat API does not support "medium" — use Responses API for that.
	ReasoningEffort *string `json:"reasoningEffort,omitempty"`

	// ParallelFunctionCalling controls whether the model may call multiple tools
	// in a single turn. Defaults to true when tools are provided.
	ParallelFunctionCalling *bool `json:"parallelFunctionCalling,omitempty"`

	// SearchParameters configures the Live Search / web search behavior.
	SearchParameters *XAIChatSearchParameters `json:"searchParameters,omitempty"`
}

// XAIChatSearchParameters configures the XAI Live Search feature.
type XAIChatSearchParameters struct {
	// Mode controls when search is triggered: "auto", "on", "off".
	Mode string `json:"mode"`

	// ReturnCitations includes source citations in the response when true.
	ReturnCitations *bool `json:"returnCitations,omitempty"`

	// FromDate limits results to content published after this date (YYYY-MM-DD).
	FromDate *string `json:"fromDate,omitempty"`

	// ToDate limits results to content published before this date (YYYY-MM-DD).
	ToDate *string `json:"toDate,omitempty"`

	// MaxSearchResults limits the number of search results considered.
	MaxSearchResults *int `json:"maxSearchResults,omitempty"`

	// Sources specifies the search sources to use.
	Sources []XAIChatSearchSource `json:"sources,omitempty"`
}

// XAIChatSearchSource configures a single search source within SearchParameters.
type XAIChatSearchSource struct {
	// Type is the source type: "web", "x", "news", "rss".
	Type string `json:"type"`

	// Country filters results by country code (web/news sources).
	Country *string `json:"country,omitempty"`

	// ExcludedWebsites lists domains to exclude (web/news sources).
	ExcludedWebsites []string `json:"excludedWebsites,omitempty"`

	// AllowedWebsites limits results to specific domains (web source only).
	AllowedWebsites []string `json:"allowedWebsites,omitempty"`

	// SafeSearch enables safe search filtering (web/news sources).
	SafeSearch *bool `json:"safeSearch,omitempty"`

	// ExcludedXHandles lists X (Twitter) handles to exclude (x source only).
	ExcludedXHandles []string `json:"excludedXHandles,omitempty"`

	// IncludedXHandles limits results to specific X handles (x source only).
	IncludedXHandles []string `json:"includedXHandles,omitempty"`

	// XHandles is a legacy alias for IncludedXHandles. IncludedXHandles takes precedence.
	XHandles []string `json:"xHandles,omitempty"`

	// PostFavoriteCount filters X posts by minimum favorite count (x source only).
	PostFavoriteCount *int `json:"postFavoriteCount,omitempty"`

	// PostViewCount filters X posts by minimum view count (x source only).
	PostViewCount *int `json:"postViewCount,omitempty"`

	// Links specifies RSS feed URLs (rss source only).
	Links []string `json:"links,omitempty"`
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	// Extract XAI-specific provider options.
	var xaiOpts XAIChatProviderOptions
	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["xai"]; ok {
			if jsonData, err := json.Marshal(raw); err == nil {
				json.Unmarshal(jsonData, &xaiOpts) //nolint:errcheck
			}
		}
	}

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
		body["max_completion_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" {
		if opts.ResponseFormat.Schema != nil {
			name := opts.ResponseFormat.Name
			if name == "" {
				name = "response"
			}
			body["response_format"] = map[string]interface{}{
				"type": "json_schema",
				"json_schema": map[string]interface{}{
					"name":   name,
					"schema": opts.ResponseFormat.Schema,
					"strict": true,
				},
			}
		} else {
			body["response_format"] = map[string]interface{}{
				"type": "json_object",
			}
		}
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// Logprobs: setting TopLogprobs implicitly enables logprobs.
	logprobs := xaiOpts.Logprobs != nil && *xaiOpts.Logprobs
	if xaiOpts.TopLogprobs != nil {
		logprobs = true
	}
	if logprobs {
		body["logprobs"] = true
	}
	if xaiOpts.TopLogprobs != nil {
		body["top_logprobs"] = *xaiOpts.TopLogprobs
	}

	// Reasoning effort: provider option takes precedence over top-level opts.Reasoning.
	// Chat API only supports "low"|"high" (medium maps to "low").
	var effort string
	if xaiOpts.ReasoningEffort != nil {
		effort = *xaiOpts.ReasoningEffort
	} else if opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningMinimal, types.ReasoningLow, types.ReasoningMedium:
			effort = "low"
		case types.ReasoningHigh, types.ReasoningXHigh:
			effort = "high"
		// ReasoningNone → omit (no reasoning_effort field)
		}
	}
	if effort != "" {
		body["reasoning_effort"] = effort
	}

	// Parallel function calling.
	if xaiOpts.ParallelFunctionCalling != nil {
		body["parallel_function_calling"] = *xaiOpts.ParallelFunctionCalling
	}

	// Search parameters for Live Search / web search.
	if xaiOpts.SearchParameters != nil {
		sp := xaiOpts.SearchParameters
		spMap := map[string]interface{}{"mode": sp.Mode}
		if sp.ReturnCitations != nil {
			spMap["return_citations"] = *sp.ReturnCitations
		}
		if sp.FromDate != nil {
			spMap["from_date"] = *sp.FromDate
		}
		if sp.ToDate != nil {
			spMap["to_date"] = *sp.ToDate
		}
		if sp.MaxSearchResults != nil {
			spMap["max_search_results"] = *sp.MaxSearchResults
		}
		if len(sp.Sources) > 0 {
			sources := make([]map[string]interface{}, 0, len(sp.Sources))
			for _, s := range sp.Sources {
				sm := map[string]interface{}{"type": s.Type}
				switch s.Type {
				case "web":
					if s.Country != nil {
						sm["country"] = *s.Country
					}
					if len(s.ExcludedWebsites) > 0 {
						sm["excluded_websites"] = s.ExcludedWebsites
					}
					if len(s.AllowedWebsites) > 0 {
						sm["allowed_websites"] = s.AllowedWebsites
					}
					if s.SafeSearch != nil {
						sm["safe_search"] = *s.SafeSearch
					}
				case "x":
					if len(s.ExcludedXHandles) > 0 {
						sm["excluded_x_handles"] = s.ExcludedXHandles
					}
					// IncludedXHandles takes precedence; fall back to legacy XHandles alias.
					includedHandles := s.IncludedXHandles
					if len(includedHandles) == 0 {
						includedHandles = s.XHandles
					}
					if len(includedHandles) > 0 {
						sm["included_x_handles"] = includedHandles
					}
					if s.PostFavoriteCount != nil {
						sm["post_favorite_count"] = *s.PostFavoriteCount
					}
					if s.PostViewCount != nil {
						sm["post_view_count"] = *s.PostViewCount
					}
				case "news":
					if s.Country != nil {
						sm["country"] = *s.Country
					}
					if len(s.ExcludedWebsites) > 0 {
						sm["excluded_websites"] = s.ExcludedWebsites
					}
					if s.SafeSearch != nil {
						sm["safe_search"] = *s.SafeSearch
					}
				case "rss":
					if len(s.Links) > 0 {
						sm["links"] = s.Links
					}
				}
				sources = append(sources, sm)
			}
			spMap["sources"] = sources
		}
		body["search_parameters"] = spMap
	}

	return body
}

func (m *LanguageModel) convertResponse(response xaiResponse, lastAssistantMsg string) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
		}
	}
	choice := response.Choices[0]

	// Resolve primary text and any structured content parts.
	text := choice.Message.Content.Text
	// xAI sometimes echoes the last assistant message verbatim; skip it.
	if lastAssistantMsg != "" && text == lastAssistantMsg {
		text = ""
	}
	var contentParts []types.ContentPart
	if len(choice.Message.Content.Parts) > 0 {
		for _, part := range choice.Message.Content.Parts {
			switch part.Type {
			case "text":
				text += part.Text
			case "reasoning", "thinking":
				// Reasoning/thinking content blocks are surfaced as ReasoningContent
				// so callers can round-trip them through message history.
				if part.Text != "" {
					contentParts = append(contentParts, types.ReasoningContent{Text: part.Text})
				}
			default:
				// Unknown content type: wrap as CustomContent so callers can
				// inspect the raw provider data without the SDK silently dropping it.
				contentParts = append(contentParts, types.CustomContent{
					Kind:             "xai-" + part.Type,
					ProviderMetadata: part.Raw,
				})
			}
		}
	}

	// Top-level reasoning_content string on the message (Grok reasoning models).
	// This is distinct from the content parts array used by some models.
	if choice.Message.ReasoningContent != "" {
		contentParts = append(contentParts, types.ReasoningContent{Text: choice.Message.ReasoningContent})
	}

	// Map citations to SourceContent parts (type: "source", sourceType: "url").
	// Citations are returned by XAI when the Live Search / web-search tool is used.
	for i, url := range response.Citations {
		contentParts = append(contentParts, types.SourceContent{
			SourceType: "url",
			ID:         fmt.Sprintf("xai-citation-%d", i),
			URL:        url,
		})
	}

	usage := convertXaiUsage(response.Usage)

	// Expose logprobs in Usage.ProviderMetadata when present.
	if choice.Logprobs != nil {
		if usage.Raw == nil {
			usage.Raw = make(map[string]interface{})
		}
		usage.Raw["logprobs"] = choice.Logprobs
	}

	result := &types.GenerateResult{
		Text:         text,
		Content:      contentParts,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        usage,
		RawResponse:  response,
	}
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: args,
			}
		}
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("xai", 0, "", err.Error(), err)
}

func convertXaiUsage(usage xaiUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)

	// Initialize with basic values (may be updated if cached/reasoning tokens exist)
	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}

	// Handle cached tokens (from both direct field and nested structure)
	var cachedTokens int64
	if usage.CachedTokens != nil {
		cachedTokens = int64(*usage.CachedTokens)
	} else if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cachedTokens = int64(*usage.PromptTokensDetails.CachedTokens)
	}

	// Handle reasoning tokens (from both direct field and nested structure)
	var reasoningTokens int64
	if usage.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.ReasoningTokens)
	} else if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}

	// Handle text and image input tokens (from both direct fields and nested structure)
	var textTokens *int64
	var imageTokens *int64

	// First try direct fields
	if usage.TextInputTokens != nil {
		textVal := int64(*usage.TextInputTokens)
		textTokens = &textVal
	} else if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.TextTokens != nil {
		textVal := int64(*usage.PromptTokensDetails.TextTokens)
		textTokens = &textVal
	}

	if usage.ImageInputTokens != nil {
		imageVal := int64(*usage.ImageInputTokens)
		imageTokens = &imageVal
	} else if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.ImageTokens != nil {
		imageVal := int64(*usage.PromptTokensDetails.ImageTokens)
		imageTokens = &imageVal
	}

	// Set input details if we have cached or multimodal tokens
	if cachedTokens > 0 || textTokens != nil || imageTokens != nil {
		// Determine if cached tokens are inclusive (part of prompt_tokens) or exclusive (additional)
		// If cached <= prompt_tokens, they're inclusive (overlapping with prompt_tokens)
		// If cached > prompt_tokens, they're exclusive (additional to prompt_tokens)
		promptTokensIncludesCached := cachedTokens <= promptTokens

		var totalInput, noCacheTokens int64
		if promptTokensIncludesCached {
			// Cached tokens are PART of prompt_tokens
			totalInput = promptTokens
			noCacheTokens = promptTokens - cachedTokens
		} else {
			// Cached tokens are ADDITIONAL to prompt_tokens
			totalInput = promptTokens + cachedTokens
			noCacheTokens = promptTokens
		}

		// Update the total input tokens to account for inclusivity
		result.InputTokens = &totalInput

		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedTokens,
			CacheWriteTokens: nil,
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
		}
	}

	// Set output details if we have reasoning tokens
	// In XAI Chat API, reasoning_tokens are ADDITIONAL to completion_tokens
	if reasoningTokens > 0 {
		totalOutput := completionTokens + reasoningTokens
		result.OutputTokens = &totalOutput
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &completionTokens,
			ReasoningTokens: &reasoningTokens,
		}
	}

	// Build raw usage data
	if result.Raw == nil {
		result.Raw = make(map[string]interface{})
	}
	result.Raw["prompt_tokens"] = usage.PromptTokens
	result.Raw["completion_tokens"] = usage.CompletionTokens
	result.Raw["total_tokens"] = usage.TotalTokens

	// Add multimodal token counts to raw if present
	if usage.CachedTokens != nil {
		result.Raw["cached_tokens"] = int64(*usage.CachedTokens)
	}
	if usage.ReasoningTokens != nil {
		result.Raw["reasoning_tokens"] = int64(*usage.ReasoningTokens)
	}
	if usage.ImageInputTokens != nil {
		result.Raw["image_input_tokens"] = int64(*usage.ImageInputTokens)
	}
	if usage.TextInputTokens != nil {
		result.Raw["text_input_tokens"] = int64(*usage.TextInputTokens)
	}

	if usage.PromptTokensDetails != nil {
		result.Raw["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokensDetails != nil {
		result.Raw["completion_tokens_details"] = usage.CompletionTokensDetails
	}

	// Recalculate total tokens if input or output were adjusted
	if result.InputTokens != nil && result.OutputTokens != nil {
		recalculatedTotal := *result.InputTokens + *result.OutputTokens
		result.TotalTokens = &recalculatedTotal
	}

	return result
}


// xaiChoice represents a single choice in an XAI chat completion response.
type xaiChoice struct {
	Index        int             `json:"index"`
	FinishReason string          `json:"finish_reason"`
	Message      xaiMessage      `json:"message"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type xaiResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []xaiChoice `json:"choices"`
	// Citations contains URL citations returned by the XAI Live Search / web
	// search tool.  They are mapped to SourceContent parts in the result.
	Citations []string `json:"citations,omitempty"`
	Usage     xaiUsage `json:"usage"`
	// Error is a plain string returned by xAI for API-level errors.
	Error *string `json:"error,omitempty"`
	// Code is an optional error code accompanying Error.
	Code *string `json:"code,omitempty"`
}

// xaiMessage represents the message returned in an XAI chat completion choice.
// Content may be a plain string or an array of typed content blocks.
type xaiMessage struct {
	Role             string            `json:"role"`
	Content          xaiMessageContent `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []xaiToolCall     `json:"tool_calls"`
}

// xaiToolCall represents a tool call in an XAI response.
type xaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// xaiContentPart represents a single typed content block in an array-form message.
type xaiContentPart struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	Raw  json.RawMessage `json:"-"` // full raw JSON of the part for unknown types
}

// xaiMessageContent holds message content that may be a plain string or an
// array of typed content blocks.  It implements json.Unmarshaler.
type xaiMessageContent struct {
	Text  string           // set when content is a JSON string
	Parts []xaiContentPart // set when content is a JSON array
}

// UnmarshalJSON implements json.Unmarshaler for xaiMessageContent.
func (c *xaiMessageContent) UnmarshalJSON(data []byte) error {
	// Try string first — the common case.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Text = s
		return nil
	}
	// Try array of content parts.
	var rawParts []json.RawMessage
	if err := json.Unmarshal(data, &rawParts); err != nil {
		return err
	}
	c.Parts = make([]xaiContentPart, 0, len(rawParts))
	for _, raw := range rawParts {
		var part xaiContentPart
		if err := json.Unmarshal(raw, &part); err != nil {
			continue
		}
		part.Raw = raw
		c.Parts = append(c.Parts, part)
	}
	return nil
}

type xaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Detailed token counts for multimodal usage
	CachedTokens        *int `json:"cached_tokens,omitempty"`
	ReasoningTokens     *int `json:"reasoning_tokens,omitempty"`
	ImageInputTokens    *int `json:"image_input_tokens,omitempty"`
	TextInputTokens     *int `json:"text_input_tokens,omitempty"`

	// Legacy structure for backward compatibility
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

type xaiStream struct {
	*streaming.OpenAICompatStream
	lastAssistantContent string
}

// Next overrides OpenAICompatStream.Next to skip content that duplicates
// the last assistant message (an xAI-specific echo quirk).
func (s *xaiStream) Next() (*provider.StreamChunk, error) {
	chunk, err := s.OpenAICompatStream.Next()
	if err != nil {
		return nil, err
	}
	if chunk != nil && chunk.Type == provider.ChunkTypeText &&
		s.lastAssistantContent != "" && chunk.Text == s.lastAssistantContent {
		return s.Next()
	}
	return chunk, nil
}

func newXAIStream(reader io.ReadCloser, lastAssistantContent string) *xaiStream {
	s := &xaiStream{
		OpenAICompatStream:   streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason),
		lastAssistantContent: lastAssistantContent,
	}
	// Handle xAI's reasoning_content delta field, which carries reasoning text
	// from Grok reasoning models as a top-level string on the delta object.
	// Use OnReasoningDelta so the shared stream manages reasoning-start/end blocks.
	s.OnReasoningDelta = func(data []byte) (string, bool) {
		var peek struct {
			Choices []struct {
				Delta struct {
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(data, &peek) == nil && len(peek.Choices) > 0 {
			if rc := peek.Choices[0].Delta.ReasoningContent; rc != "" {
				return rc, true
			}
		}
		return "", false
	}
	// Handle top-level citations array in the last streaming chunk.
	// Citations appear alongside finish_reason and are emitted as source chunks
	// before the finish chunk.
	s.OnBeforeDelta = func(data []byte) []*provider.StreamChunk {
		var peek struct {
			Citations []string `json:"citations"`
		}
		if json.Unmarshal(data, &peek) != nil || len(peek.Citations) == 0 {
			return nil
		}
		chunks := make([]*provider.StreamChunk, 0, len(peek.Citations))
		for i, u := range peek.Citations {
			chunks = append(chunks, &provider.StreamChunk{
				Type: provider.ChunkTypeSource,
				SourceContent: &types.SourceContent{
					SourceType: "url",
					ID:         fmt.Sprintf("xai-citation-%d", i),
					URL:        u,
				},
			})
		}
		return chunks
	}
	return s
}

// lastAssistantText returns the text of the last assistant message in the
// prompt, used to detect xAI's echo-of-last-turn quirk.
func lastAssistantText(opts *provider.GenerateOptions) string {
	if !opts.Prompt.IsMessages() {
		return ""
	}
	msgs := opts.Prompt.Messages
	if len(msgs) == 0 {
		return ""
	}
	last := msgs[len(msgs)-1]
	if last.Role != types.RoleAssistant {
		return ""
	}
	for _, part := range last.Content {
		if tc, ok := part.(types.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

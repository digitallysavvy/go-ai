package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements the provider.LanguageModel interface for Fireworks AI
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Fireworks AI language model
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
	return "fireworks"
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
	reqBody := m.buildRequestBody(opts, false)
	var response fireworksResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody := m.buildRequestBody(opts, true)
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
	stream := newFireworksStream(httpResp.Body)
	stream.SetRequestBody(reqBody)
	return providerutils.WithResponseMetadata(stream, httpResp.Header, m.ModelID()), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}
	// AllowVideo: true -- Fireworks wraps @ai-sdk/openai-compatible's
	// OpenAICompatibleChatLanguageModel in TS, which supports video_url
	// content parts (7dd9ec320c).
	toOpenAIMessagesOpts := prompt.ToOpenAIMessagesOptions{AllowVideo: true}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages, toOpenAIMessagesOpts)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text), toOpenAIMessagesOpts)
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
	if len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	// Response format (TS openai-compatible chat model with
	// supportsStructuredOutputs: true): json_schema when a schema is present,
	// strictJsonSchema (default true) from the fireworks provider options.
	fireworksOptions, _ := providerutils.ResolveOpenAICompatibleProviderOptions("fireworks", opts.ProviderOptions)
	if format, _ := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs: true,
		StrictJSONSchema:  providerutils.BoolOption(fireworksOptions, "strictJsonSchema", true),
	}); format != nil {
		body["response_format"] = format
	}
	// Map top-level Reasoning to Fireworks reasoning_effort.
	// TS (openai-compatible-chat-language-model.ts:310-312): reasoning_effort
	// is `compatibleOptions.reasoningEffort ?? (isCustomReasoning(reasoning)
	// ? reasoning : undefined)` -- an explicit providerOptions.fireworks.
	// reasoningEffort always wins over the unified Reasoning field.
	// isCustomReasoning excludes only undefined/'provider-default', NOT
	// 'none', so 'none' is forwarded as "none", not omitted. Fireworks' own
	// transformRequestBody then remaps only minimal→low and xhigh→high,
	// passing every other value (including an override with an
	// unrecognized/custom string) straight through
	// (fireworks-provider.ts:169-177).
	reasoningEffort, hasReasoningEffort := providerutils.OpenAICompatibleStringOption(fireworksOptions, "reasoningEffort")
	if !hasReasoningEffort && opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningNone:
			reasoningEffort, hasReasoningEffort = "none", true
		case types.ReasoningMinimal:
			reasoningEffort, hasReasoningEffort = "minimal", true
		case types.ReasoningLow:
			reasoningEffort, hasReasoningEffort = "low", true
		case types.ReasoningMedium:
			reasoningEffort, hasReasoningEffort = "medium", true
		case types.ReasoningHigh:
			reasoningEffort, hasReasoningEffort = "high", true
		case types.ReasoningXHigh:
			reasoningEffort, hasReasoningEffort = "xhigh", true
			// ReasoningDefault: omit
		}
	}
	if hasReasoningEffort {
		switch reasoningEffort {
		case "minimal":
			reasoningEffort = "low"
		case "xhigh":
			reasoningEffort = "high"
		}
		body["reasoning_effort"] = reasoningEffort
	}

	// Fireworks-specific options (providerOptions.fireworks; TS
	// fireworksLanguageModelOptions/fireworks-provider.ts's
	// transformRequestBody). All of these previously read from
	// opts.ProviderOptions[...] directly (the top-level namespace) instead of
	// the "fireworks" provider-options namespace resolved above, so they were
	// silently ignored whenever a caller correctly namespaced them under
	// providerOptions.fireworks.
	if thinking, ok := fireworksOptions["thinking"].(map[string]interface{}); ok {
		thinkingBody := make(map[string]interface{})

		if thinkingType, ok := thinking["type"].(string); ok {
			thinkingBody["type"] = thinkingType
		}

		// Convert budgetTokens (camelCase) to budget_tokens (snake_case)
		if budgetTokens, ok := providerutils.OpenAICompatibleIntOption(thinking, "budgetTokens"); ok {
			thinkingBody["budget_tokens"] = budgetTokens
		}

		if len(thinkingBody) > 0 {
			body["thinking"] = thinkingBody
		}
	}

	// Extract reasoningHistory and convert to snake_case (reasoning_history)
	if reasoningHistory, ok := providerutils.OpenAICompatibleStringOption(fireworksOptions, "reasoningHistory"); ok {
		body["reasoning_history"] = reasoningHistory
	}

	// A stable key for routing requests with shared prompt prefixes to the
	// same prompt cache.
	if promptCacheKey, ok := providerutils.OpenAICompatibleStringOption(fireworksOptions, "promptCacheKey"); ok {
		body["prompt_cache_key"] = promptCacheKey
	}

	// Fireworks Priority serving path for higher reliability during peak
	// traffic.
	if serviceTier, ok := providerutils.OpenAICompatibleStringOption(fireworksOptions, "serviceTier"); ok {
		body["service_tier"] = serviceTier
	}

	return body
}

func (m *LanguageModel) convertResponse(response fireworksResponse) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
		}
	}
	choice := response.Choices[0]
	result := &types.GenerateResult{
		Text:            choice.Message.Content,
		FinishReason:    providerutils.MapOpenAIFinishReason(choice.FinishReason),
		RawFinishReason: choice.FinishReason,
		Usage:           convertFireworksUsage(response.Usage),
		RawResponse:     response,
	}
	if choice.Message.ReasoningContent != "" {
		result.Content = append(result.Content, types.ReasoningContent{Text: choice.Message.ReasoningContent})
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
	if parsed := parseFireworksProviderError(err); parsed != nil {
		return parsed
	}
	// TS createJsonErrorResponseHandler always sets statusCode: response.status
	// even in its catch-all branch; preserve the real HTTP status here too
	// instead of hardcoding 0 (which several retry-classification paths treat
	// as "unknown", i.e. retryable).
	statusCode := 0
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		statusCode = statusErr.StatusCode
	}
	return providererrors.NewProviderError("fireworks", statusCode, "", err.Error(), err)
}

// convertFireworksUsage converts Fireworks usage to detailed Usage struct
func convertFireworksUsage(usage fireworksUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)
	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}
	var cachedTokens int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cachedTokens = int64(*usage.PromptTokensDetails.CachedTokens)
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
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}
	if cachedTokens > 0 || textTokens != nil || imageTokens != nil {
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedTokens,
			CacheWriteTokens: nil,
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
		}
	}
	if reasoningTokens > 0 {
		textTokens := completionTokens - reasoningTokens
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		}
	}
	result.Raw = map[string]interface{}{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		result.Raw["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokensDetails != nil {
		result.Raw["completion_tokens_details"] = usage.CompletionTokensDetails
	}
	return result
}

type fireworksResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage fireworksUsage `json:"usage"`
}

// fireworksUsage represents Fireworks usage information with detailed token tracking
type fireworksUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
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

type fireworksStream struct {
	*streaming.OpenAICompatStream
}

func newFireworksStream(reader io.ReadCloser) *fireworksStream {
	s := streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason)
	s.OnReasoningDelta = func(eventBytes []byte) (string, bool) {
		var chunk struct {
			Choices []struct {
				Delta struct {
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(eventBytes, &chunk); err != nil || len(chunk.Choices) == 0 {
			return "", false
		}
		rc := chunk.Choices[0].Delta.ReasoningContent
		return rc, rc != ""
	}
	return &fireworksStream{OpenAICompatStream: s}
}

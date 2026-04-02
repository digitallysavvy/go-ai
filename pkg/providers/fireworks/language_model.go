package fireworks

import (
	"context"
	"encoding/json"
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
	return providerutils.WithResponseMetadata(newFireworksStream(httpResp.Body), httpResp.Header, m.ModelID()), nil
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
	if len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	if opts.ResponseFormat != nil {
		body["response_format"] = map[string]interface{}{
			"type": opts.ResponseFormat.Type,
		}
	}
	// Map top-level Reasoning to Fireworks reasoning_effort.
	// none and provider-default → omit (Fireworks passes through the raw level string
	// and only accepts supported values; none is excluded at the base layer).
	// minimal/low → "low", medium → "medium", high/xhigh → "high".
	if opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningMinimal, types.ReasoningLow:
			body["reasoning_effort"] = "low"
		case types.ReasoningMedium:
			body["reasoning_effort"] = "medium"
		case types.ReasoningHigh, types.ReasoningXHigh:
			body["reasoning_effort"] = "high"
			// ReasoningNone and ReasoningDefault: omit
		}
	}

	// Handle Fireworks-specific options (thinking and reasoning for Kimi K2.5)
	if opts.ProviderOptions != nil {
		// Extract thinking options
		if thinking, ok := opts.ProviderOptions["thinking"].(map[string]interface{}); ok {
			thinkingBody := make(map[string]interface{})

			if thinkingType, ok := thinking["type"].(string); ok {
				thinkingBody["type"] = thinkingType
			}

			// Convert budgetTokens (camelCase) to budget_tokens (snake_case)
			if budgetTokens, ok := thinking["budgetTokens"].(int); ok {
				thinkingBody["budget_tokens"] = budgetTokens
			} else if budgetTokens, ok := thinking["budgetTokens"].(float64); ok {
				thinkingBody["budget_tokens"] = int(budgetTokens)
			}

			if len(thinkingBody) > 0 {
				body["thinking"] = thinkingBody
			}
		}

		// Extract reasoningHistory and convert to snake_case (reasoning_history)
		if reasoningHistory, ok := opts.ProviderOptions["reasoningHistory"].(string); ok {
			body["reasoning_history"] = reasoningHistory
		}
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
		Text:         choice.Message.Content,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        convertFireworksUsage(response.Usage),
		RawResponse:  response,
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
	return providererrors.NewProviderError("fireworks", 0, "", err.Error(), err)
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

package ollama

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

// LanguageModel implements the provider.LanguageModel interface for Ollama
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Ollama language model
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "ollama"
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
	if err := prompt.ValidateProviderFileReferences(opts.Prompt.Messages, "ollama"); err != nil {
		return nil, err
	}
	reqBody, warnings := m.buildRequestBody(opts, false)
	var response ollamaResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Body:    reqBody,
		Headers: opts.Headers,
	}, &streaming.OpenAICompatChatDecoder{Target: &response})
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response)
	if err := streaming.ApplyOpenAICompatibleChatResponse(result, resp.Body, providerutils.ExtractHeaders(resp.Headers), "ollama"); err != nil {
		return nil, err
	}
	providerutils.ApplyWireUsageRaw(result, resp.Body)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	// TS result.request.body: the request as sent.
	result.RawRequest = reqBody
	// TS response.body is the raw parsed JSON, not the schema-filtered value.
	if len(resp.Body) > 0 {
		result.RawResponse = providerutils.RawJSONBody(resp.Body)
	}
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if err := prompt.ValidateProviderFileReferences(opts.Prompt.Messages, "ollama"); err != nil {
		return nil, err
	}
	reqBody, warnings := m.buildRequestBody(opts, true)
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Body:    reqBody,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	stream := newOllamaStream(httpResp.Body)
	stream.SetRequestBody(reqBody)
	stream.IncludeRawChunks = opts.IncludeRawChunks
	stream.ResponseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return streaming.NewWarningsStream(stream, warnings), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning) {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages, prompt.ToOpenAIMessagesOptions{AllowVideo: true, Provider: "ollama", ThoughtSignatureKey: "ollama", ReasoningKey: "reasoning_content", SpreadCompatMetadata: true})
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text), prompt.ToOpenAIMessagesOptions{AllowVideo: true, Provider: "ollama", ThoughtSignatureKey: "ollama", ReasoningKey: "reasoning_content", SpreadCompatMetadata: true})
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
	// TS prepareTools (openai-compatible): a provider-defined tool is dropped
	// with an `unsupported` warning instead of being sent as a function tool.
	var warnings []types.Warning
	if len(opts.Tools) > 0 {
		functionTools, dropped := tool.DropProviderDefinedTools(opts.Tools, nil)
		warnings = append(warnings, dropped...)
		body["tools"] = tool.ToOpenAIFormat(functionTools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	// Penalties and seed are sent, topK warns, unknown providerOptions.ollama
	// keys are spread into the body (TS openai-compatible getArgs).
	warnings = append(warnings, providerutils.ApplyOpenAICompatibleCallSettings(body, opts.FrequencyPenalty, opts.PresencePenalty, opts.Seed, opts.TopK, "ollama", opts.ProviderOptions)...)
	// "json" is not a Chat Completions response_format type: json becomes
	// json_object, or json_schema when a schema is given.
	compatibleOptions, _ := providerutils.ResolveOpenAICompatibleProviderOptions("ollama", opts.ProviderOptions)
	responseFormat, formatWarnings := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs: true,
		StrictJSONSchema:  providerutils.BoolOption(compatibleOptions, "strictJsonSchema", true),
	})
	if responseFormat != nil {
		body["response_format"] = responseFormat
	}
	warnings = append(warnings, formatWarnings...)
	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		body["reasoning_effort"] = string(*opts.Reasoning)
	}
	providerutils.ApplyOpenAICompatibleCommonRequestOptions(body, compatibleOptions)
	return body, warnings
}

func (m *LanguageModel) convertResponse(response ollamaResponse) *types.GenerateResult {
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
		Usage:           convertOllamaUsage(response.Usage),
		RawResponse:     response,
	}
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:           tc.ID,
				ToolName:     tc.Function.Name,
				Arguments:    args,
				RawArguments: tc.Function.Arguments,
			}
		}
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	return providererrors.NewProviderError("ollama", 0, "", err.Error(), err)
}

func convertOllamaUsage(usage ollamaUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := promptTokens + completionTokens // TS core: input total + output total
	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}
	var cachedTokens int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cachedTokens = int64(*usage.PromptTokensDetails.CachedTokens)
	}
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}
	{
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedTokens,
			CacheWriteTokens: nil,
		}
	}
	{
		textTokens := completionTokens - reasoningTokens
		if textTokens < 0 {
			textTokens = 0
		}
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

type ollamaResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage ollamaUsage `json:"usage"`
}

type ollamaUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type ollamaStream struct {
	*streaming.OpenAICompatStream
}

func newOllamaStream(reader io.ReadCloser) *ollamaStream {
	s := &ollamaStream{
		OpenAICompatStream: streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason),
	}
	s.MetadataKey = "ollama"
	return s
}

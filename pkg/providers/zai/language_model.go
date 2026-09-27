package zai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements the provider.LanguageModel interface for Z.AI
// chat completions. TS builds ZaiChatLanguageModel on
// @ai-sdk/openai-compatible (OpenAICompatibleChatLanguageModel) with a
// transformRequestBody hook and finish-reason remapping; this mirrors that
// with its own request/response handling plus the shared OpenAICompatStream
// base for streaming (accumulate + flush tool calls, no per-delta
// emission), matching the fireworks/together/gmicloud Go providers.
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Z.AI language model.
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *LanguageModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider name.
func (m *LanguageModel) Provider() string { return "zai" }

// ModelID returns the model ID.
func (m *LanguageModel) ModelID() string { return m.modelID }

// SupportsTools returns whether the model supports tool calling.
func (m *LanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput returns whether the model supports json_schema
// structured outputs. Z.AI's OpenAICompatibleChatLanguageModel configuration
// does not set supportsStructuredOutputs, so it defaults to false (JSON mode
// always falls back to {"type":"json_object"}).
func (m *LanguageModel) SupportsStructuredOutput() bool { return false }

// SupportsImageInput returns whether the model accepts image inputs.
func (m *LanguageModel) SupportsImageInput() bool { return false }

// SupportedURLs returns the URL patterns this model accepts directly without
// downloading first. Mirrors TS ZaiChatLanguageModel's supportedUrls
// override (image/* and video/* over https).
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*": {`^https?://.*$`},
		"video/*": {`^https?://.*$`},
	}
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, warnings, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, err
	}
	var response zaiResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: stdhttp.MethodPost,
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

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody, warnings, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, err
	}
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  stdhttp.MethodPost,
		Path:    "/chat/completions",
		Body:    reqBody,
		Headers: map[string]string{"Accept": "text/event-stream"},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	inner := newZaiStream(httpResp.Body)
	inner.IncludeRawChunks = opts.IncludeRawChunks
	inner.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return streaming.NewWarningsStream(inner, warnings), nil
}

func isCustomReasoning(r *types.ReasoningLevel) bool {
	return r != nil && *r != types.ReasoningDefault
}

// buildRequestBody constructs the Z.AI chat completions request body. It
// mirrors TS ZaiChatLanguageModel.prepareCallOptions (frequencyPenalty /
// presencePenalty / seed dropped with warnings, toolChoice restricted to
// "auto") plus transformZaiRequestBody (doSample -> do_sample, thinking ->
// {type, clear_thinking}, toolStream -> tool_stream, requestId ->
// request_id, userId -> user_id).
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
	}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}
	if opts.Prompt.System != "" {
		messages, _ := body["messages"].([]map[string]interface{})
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

	var warnings []types.Warning

	// frequencyPenalty/presencePenalty/seed are never supported by Z.AI (TS
	// prepareCallOptions always strips them and warns when set).
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}

	// toolChoice: Z.AI only supports automatic tool selection.
	// type "none" additionally drops the tool list entirely.
	tools := opts.Tools
	toolChoice := opts.ToolChoice
	switch {
	case toolChoice.Type == types.ToolChoiceNone:
		tools = nil
		toolChoice = types.ToolChoice{}
	case toolChoice.Type != "" && toolChoice.Type != types.ToolChoiceAuto:
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "toolChoice " + string(toolChoice.Type),
			Details: "Z.AI currently supports only automatic tool selection.",
		})
		toolChoice = types.ToolChoice{}
	}
	if len(tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(tools)
	}
	if toolChoice.Type != "" {
		body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(toolChoice)
	}

	compatibleOptions, resolveWarnings := providerutils.ResolveOpenAICompatibleProviderOptions("zai", opts.ProviderOptions)
	warnings = append(warnings, resolveWarnings...)
	warnings = append(warnings, providerutils.OpenAICompatibleCommonOptionWarnings(compatibleOptions)...)

	responseFormat, formatWarnings := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs:         false,
		StrictJSONSchema:          providerutils.BoolOption(compatibleOptions, "strictJsonSchema", true),
		WarnWhenSchemaUnsupported: true,
	})
	if responseFormat != nil {
		body["response_format"] = responseFormat
	}
	warnings = append(warnings, formatWarnings...)

	// Map top-level Reasoning to reasoning_effort when the caller hasn't set
	// providerOptions.zai.reasoningEffort explicitly.
	if isCustomReasoning(opts.Reasoning) {
		body["reasoning_effort"] = string(*opts.Reasoning)
	}
	providerutils.ApplyOpenAICompatibleCommonRequestOptions(body, compatibleOptions)

	zaiOpts, err := decodeZaiOptions(compatibleOptions)
	if err != nil {
		return nil, nil, err
	}
	if zaiOpts.doSample != nil {
		body["do_sample"] = *zaiOpts.doSample
	}
	if zaiOpts.thinking != nil {
		thinkingBody := map[string]interface{}{}
		if zaiOpts.thinking.typ != "" {
			thinkingBody["type"] = zaiOpts.thinking.typ
		}
		if zaiOpts.thinking.clearThinking != nil {
			thinkingBody["clear_thinking"] = *zaiOpts.thinking.clearThinking
		}
		if len(thinkingBody) > 0 {
			body["thinking"] = thinkingBody
		}
	}
	if zaiOpts.toolStream != nil {
		body["tool_stream"] = *zaiOpts.toolStream
	}
	if zaiOpts.requestID != "" {
		body["request_id"] = zaiOpts.requestID
	}
	if zaiOpts.userID != "" {
		body["user_id"] = zaiOpts.userID
	}

	return body, warnings, nil
}

func (m *LanguageModel) convertResponse(response zaiResponse) *types.GenerateResult {
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
		Usage:        convertZaiUsage(response.Usage),
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
	if response.ID != "" || response.Model != "" || response.Created != 0 {
		meta := &types.ResponseMetadata{ID: response.ID, ModelID: response.Model}
		if response.Created != 0 {
			meta.Timestamp = time.Unix(response.Created, 0).UTC()
		}
		result.ResponseMetadata = meta
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	if parsed := parseZaiProviderError(err); parsed != nil {
		return parsed
	}
	statusCode := 0
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		statusCode = statusErr.StatusCode
	}
	return providererrors.NewProviderError("zai", statusCode, "", err.Error(), err)
}

func convertZaiUsage(usage zaiUsage) types.Usage {
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
	if cachedTokens > 0 {
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedTokens,
		}
	}
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
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
	return result
}

type zaiResponse struct {
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
	Usage zaiUsage `json:"usage"`
}

type zaiUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type zaiStream struct {
	*streaming.OpenAICompatStream
	responseHeaders         map[string]string
	responseMetadataEmitted bool
}

// newZaiStream wraps the shared OpenAICompatStream, adding Z.AI's
// reasoning_content delta extraction and a one-shot response-metadata chunk
// derived from the first SSE event (id/model/created), matching the
// together/gmicloud Go providers. Finish-reason mapping (including Z.AI's
// "sensitive"/"model_context_window_exceeded"/"network_error" reasons) goes
// through the shared providerutils.MapOpenAIFinishReason, which the stream
// base calls automatically at finish.
func newZaiStream(reader io.ReadCloser) *zaiStream {
	s := &zaiStream{
		OpenAICompatStream: streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason),
	}
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
	s.OnBeforeDelta = func(data []byte) []*provider.StreamChunk {
		var peek struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
		}
		if json.Unmarshal(data, &peek) != nil {
			return nil
		}
		if s.responseMetadataEmitted || (peek.ID == "" && peek.Model == "" && peek.Created == 0 && len(s.responseHeaders) == 0) {
			return nil
		}
		s.responseMetadataEmitted = true
		meta := &provider.ResponseMetadata{
			ID:      peek.ID,
			ModelID: peek.Model,
			Headers: s.responseHeaders,
		}
		if peek.Created != 0 {
			meta.Timestamp = time.Unix(peek.Created, 0)
		}
		return []*provider.StreamChunk{{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: meta,
		}}
	}
	return s
}

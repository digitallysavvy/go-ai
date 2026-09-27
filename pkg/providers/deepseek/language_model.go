package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// deepseekUserIDPattern validates providerOptions.deepseek.userId.
var deepseekUserIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// LanguageModel implements the provider.LanguageModel interface for Deepseek
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Deepseek language model
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
	return m.provider.Name()
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

// SupportsImageInput returns whether the model accepts image inputs.
// DeepSeek chat models declare image/* support unconditionally (matches the
// TypeScript SDK's supportedUrls); whether a given deployment actually
// accepts images is a model-serving concern, not something the SDK gates.
func (m *LanguageModel) SupportsImageInput() bool {
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		return nil, err
	}
	var response deepseekResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   m.provider.chatCompletionsPath(),
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	responseMetadata := &types.ResponseMetadata{
		ID:      response.ID,
		ModelID: response.Model,
		Headers: result.ResponseHeaders,
	}
	if response.Created != 0 {
		responseMetadata.Timestamp = time.Unix(response.Created, 0)
	}
	result.ResponseMetadata = responseMetadata
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts, true)
	if err != nil {
		return nil, err
	}
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   m.provider.chatCompletionsPath(),
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	inner := newDeepseekStream(httpResp.Body, opts.IncludeRawChunks)
	inner.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	inner.providerOptionsName = m.provider.providerOptionsName()
	return streaming.NewWarningsStream(inner, warnings), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body, _, _ := m.buildRequestBodyWithWarnings(opts, stream)
	return body
}

func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	var messages []types.Message
	if opts.Prompt.IsMessages() {
		messages = opts.Prompt.Messages
	} else if opts.Prompt.IsSimple() {
		messages = prompt.SimpleTextToMessages(opts.Prompt.Text)
	}
	if opts.Prompt.System != "" {
		systemMsg := types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}}}
		messages = append([]types.Message{systemMsg}, messages...)
	}
	convertedMessages, msgWarnings, err := m.convertMessages(messages)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, msgWarnings...)
	body["messages"] = convertedMessages

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

	deepseekTools, toolChoiceValue, toolWarnings, err := m.prepareDeepSeekTools(opts.Tools, opts.ToolChoice)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, toolWarnings...)
	if deepseekTools != nil {
		body["tools"] = deepseekTools
	}
	if toolChoiceValue != nil {
		body["tool_choice"] = toolChoiceValue
	}

	deepseekOptions, optionWarnings := providerutils.ResolveOpenAICompatibleProviderOptions(m.provider.providerOptionsName(), opts.ProviderOptions)
	warnings = append(warnings, optionWarnings...)

	// frequency_penalty/presence_penalty are deprecated by the upstream
	// DeepSeek API, but Azure-hosted DeepSeek deployments still accept them
	// (TS: deepseek-chat-language-model.ts supportsPenaltySampling).
	supportsPenaltySampling := m.provider.supportsPenaltySampling()
	if supportsPenaltySampling {
		if opts.FrequencyPenalty != nil {
			body["frequency_penalty"] = *opts.FrequencyPenalty
		}
		if opts.PresencePenalty != nil {
			body["presence_penalty"] = *opts.PresencePenalty
		}
	} else {
		if opts.FrequencyPenalty != nil {
			const msg = "frequencyPenalty is deprecated by DeepSeek and has been omitted. Remove frequencyPenalty from the request."
			warnings = append(warnings, types.Warning{
				Type:    "deprecated",
				Setting: "frequencyPenalty",
				Details: msg,
				Message: msg,
			})
		}
		if opts.PresencePenalty != nil {
			const msg = "presencePenalty is deprecated by DeepSeek and has been omitted. Remove presencePenalty from the request."
			warnings = append(warnings, types.Warning{
				Type:    "deprecated",
				Setting: "presencePenalty",
				Details: msg,
				Message: msg,
			})
		}
	}

	// response_format: Azure-hosted DeepSeek deployments support json_schema
	// structured outputs; the upstream DeepSeek API only supports json_object
	// (TS: deepseek-chat-language-model.ts supportsStructuredOutputs).
	if opts.ResponseFormat != nil {
		switch {
		case opts.ResponseFormat.Type == "json" && m.provider.supportsStructuredOutputs() && opts.ResponseFormat.Schema != nil:
			name := opts.ResponseFormat.Name
			if name == "" {
				name = "response"
			}
			strict := true
			if v, ok := providerutils.OpenAICompatibleStringOption(deepseekOptions, "strictJsonSchema"); ok {
				strict = v == "true"
			} else if v, ok := deepseekOptions["strictJsonSchema"].(bool); ok {
				strict = v
			}
			jsonSchema := map[string]interface{}{
				"schema": opts.ResponseFormat.Schema,
				"strict": strict,
				"name":   name,
			}
			if opts.ResponseFormat.Description != "" {
				jsonSchema["description"] = opts.ResponseFormat.Description
			}
			body["response_format"] = map[string]interface{}{
				"type":        "json_schema",
				"json_schema": jsonSchema,
			}
		case opts.ResponseFormat.Type != "":
			responseFormatType := opts.ResponseFormat.Type
			if responseFormatType == "json" {
				responseFormatType = "json_object"
			}
			body["response_format"] = map[string]interface{}{
				"type": responseFormatType,
			}
		}
	}
	_, hasProviderReasoningEffort := providerutils.OpenAICompatibleStringOption(deepseekOptions, "reasoningEffort")

	// Map top-level Reasoning to DeepSeek thinking + reasoning_effort (TS parity).
	// TS effort map: minimal/low -> low, medium/high -> high, xhigh -> max.
	if opts.Reasoning != nil {
		if *opts.Reasoning == types.ReasoningNone {
			if m.provider.supportsThinking() {
				body["thinking"] = map[string]interface{}{"type": "disabled"}
			}
		} else if effort, ok := deepseekTopLevelReasoningEffort(*opts.Reasoning); ok {
			if m.provider.supportsThinking() {
				body["thinking"] = map[string]interface{}{"type": "enabled"}
			}
			body["reasoning_effort"] = effort
			if effort != string(*opts.Reasoning) && !hasProviderReasoningEffort {
				warnings = append(warnings, reasoningCompatibilityWarning(string(*opts.Reasoning), effort))
			}
		}
	}

	if thinking, ok := deepseekOptions["thinking"].(map[string]interface{}); ok && m.provider.supportsThinking() {
		if thinkingType, ok := providerutils.OpenAICompatibleStringOption(thinking, "type"); ok {
			mappedType := thinkingType
			if thinkingType == "adaptive" {
				mappedType = "enabled"
				warnings = append(warnings, types.Warning{
					Type:    "compatibility",
					Feature: "thinking.type",
					Details: `thinking.type "adaptive" is not a canonical DeepSeek value. mapped to "enabled".`,
					Message: `thinking.type "adaptive" is not a canonical DeepSeek value. mapped to "enabled".`,
				})
			}
			body["thinking"] = map[string]interface{}{"type": mappedType}
		}
	}
	if effort, ok := providerutils.OpenAICompatibleStringOption(deepseekOptions, "reasoningEffort"); ok {
		mapped, remapped := deepseekProviderReasoningEffort(effort)
		if remapped {
			details := fmt.Sprintf("reasoningEffort %q is not a canonical DeepSeek value. mapped to %q.", effort, mapped)
			warnings = append(warnings, types.Warning{Type: "compatibility", Feature: "reasoningEffort", Details: details, Message: details})
		}
		body["reasoning_effort"] = mapped
	}
	thinkingDisabled := false
	if thinking, ok := body["thinking"].(map[string]interface{}); ok {
		if thinking["type"] == "disabled" {
			thinkingDisabled = true
			delete(body, "reasoning_effort")
		}
	}

	// TS: temperature/topP have no effect once DeepSeek thinking is enabled
	// (and are omitted from the wire body, with an "unsupported" warning),
	// where isThinkingEnabled is true whenever thinking wasn't explicitly
	// disabled and either a thinking object was resolved above or the model
	// defaults to thinking (deepseek-reasoner, or any V4 model).
	isThinkingEnabled := m.provider.supportsThinking() && !thinkingDisabled &&
		(body["thinking"] != nil || m.modelID == "deepseek-reasoner" || isDeepSeekV4Model(m.modelID))
	if isThinkingEnabled {
		if _, has := body["temperature"]; has {
			delete(body, "temperature")
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "temperature",
				Details: "temperature has no effect when DeepSeek thinking is enabled. Set providerOptions.deepseek.thinking.type to 'disabled' to use temperature.",
				Message: "temperature has no effect when DeepSeek thinking is enabled. Set providerOptions.deepseek.thinking.type to 'disabled' to use temperature.",
			})
		}
		if _, has := body["top_p"]; has {
			delete(body, "top_p")
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "topP",
				Details: "topP has no effect when DeepSeek thinking is enabled. Set providerOptions.deepseek.thinking.type to 'disabled' to use topP.",
				Message: "topP has no effect when DeepSeek thinking is enabled. Set providerOptions.deepseek.thinking.type to 'disabled' to use topP.",
			})
		}
	}

	// userId (providerOptions.deepseek.userId): opaque end-user identifier.
	if userID, ok := providerutils.OpenAICompatibleStringOption(deepseekOptions, "userId"); ok {
		if !deepseekUserIDPattern.MatchString(userID) {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions.deepseek.userId",
				Message: "userId must match /^[a-zA-Z0-9_-]+$/",
			}
		}
		if len(userID) > 512 {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions.deepseek.userId",
				Message: "userId must be at most 512 characters long",
			}
		}
		body["user_id"] = userID
	}

	// logprobs / topLogprobs.
	logprobsFlag, hasLogprobsFlag := providerutils.OpenAICompatibleBoolOption(deepseekOptions, "logprobs")
	if _, hasTopLogprobs := deepseekOptions["topLogprobs"]; hasTopLogprobs {
		topLogprobs, ok := providerutils.OpenAICompatibleIntOption(deepseekOptions, "topLogprobs")
		if !ok || topLogprobs < 0 || topLogprobs > 20 {
			return nil, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions.deepseek.topLogprobs",
				Message: "topLogprobs must be an integer between 0 and 20.",
			}
		}
		body["logprobs"] = true
		body["top_logprobs"] = topLogprobs
	} else if hasLogprobsFlag && logprobsFlag {
		body["logprobs"] = true
	}

	return body, warnings, nil
}

// deepseekTopLevelReasoningEffort maps a top-level ReasoningLevel to
// DeepSeek's reasoning_effort values, mirroring the TypeScript SDK's
// mapReasoningToProviderEffort({minimal:'low', low:'low', medium:'high',
// high:'high', xhigh:'max'}). ReasoningDefault (and any other unmapped level)
// returns ok=false.
func deepseekTopLevelReasoningEffort(level types.ReasoningLevel) (effort string, ok bool) {
	switch level {
	case types.ReasoningMinimal, types.ReasoningLow:
		return "low", true
	case types.ReasoningMedium, types.ReasoningHigh:
		return "high", true
	case types.ReasoningXHigh:
		return "max", true
	}
	return "", false
}

// deepseekProviderReasoningEffort remaps legacy providerOptions.deepseek.reasoningEffort
// values to DeepSeek's canonical set ({low, high, max}), mirroring the
// TypeScript SDK's mapDeepSeekProviderReasoningEffort. remapped is true when
// the input was not already canonical (used to decide whether to warn).
func deepseekProviderReasoningEffort(effort string) (mapped string, remapped bool) {
	switch effort {
	case "medium":
		return "high", true
	case "xhigh":
		return "max", true
	default:
		return effort, false
	}
}

func reasoningCompatibilityWarning(reasoning, effort string) types.Warning {
	details := fmt.Sprintf("reasoning %q is not directly supported by this model. mapped to effort %q.", reasoning, effort)
	return types.Warning{
		Type:    "compatibility",
		Feature: "reasoning",
		Details: details,
		Message: details,
	}
}

func (m *LanguageModel) convertResponse(response deepseekResponse) (*types.GenerateResult, error) {
	if len(response.Choices) == 0 {
		return nil, providererrors.NewInvalidResponseDataError(response, "Response did not contain any choices.")
	}
	choice := response.Choices[0]
	result := &types.GenerateResult{
		Text:         choice.Message.Content,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        convertDeepseekUsage(response.Usage),
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
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: args,
			}
		}
	}

	meta := map[string]interface{}{}
	hitTokens, missTokens := deepseekPromptCacheTokens(response.Usage)
	if hitTokens != nil {
		meta["promptCacheHitTokens"] = *hitTokens
	}
	if missTokens != nil {
		meta["promptCacheMissTokens"] = *missTokens
	}
	if response.Object != "" {
		meta["responseObject"] = response.Object
	}
	meta["choiceIndex"] = choice.Index
	if choice.Message.Role != "" {
		meta["messageRole"] = choice.Message.Role
	}
	if len(choice.Message.ToolCalls) > 0 {
		toolCallTypes := make([]string, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			if tc.Type != "" {
				toolCallTypes = append(toolCallTypes, tc.Type)
			}
		}
		if len(toolCallTypes) > 0 {
			meta["toolCallTypes"] = toolCallTypes
		}
	}
	if choice.Logprobs != nil {
		lp := map[string]interface{}{}
		if len(choice.Logprobs.Content) > 0 {
			lp["content"] = choice.Logprobs.Content
		}
		if len(choice.Logprobs.ReasoningContent) > 0 {
			lp["reasoning_content"] = choice.Logprobs.ReasoningContent
		}
		if len(lp) > 0 {
			meta["logprobs"] = lp
		}
	}
	if response.SystemFingerprint != "" {
		meta["systemFingerprint"] = response.SystemFingerprint
	}
	result.ProviderMetadata = map[string]interface{}{m.provider.providerOptionsName(): meta}

	return result, nil
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("deepseek", 0, "", err.Error(), err)
}

// convertDeepseekUsage decodes the DeepSeek usage object twice: once into a
// typed struct for computing normalized token metrics, and once into a
// map[string]interface{} so Usage.Raw preserves every field the API
// returned (not just the ones the typed struct declares).
// deepseekPromptCacheTokens extracts prompt_cache_hit_tokens/
// prompt_cache_miss_tokens from a raw usage object for providerMetadata.
// deepseek.{promptCacheHitTokens,promptCacheMissTokens}, mirroring TS
// deepseek-chat-language-model.ts's responseBody.usage?.prompt_cache_hit_tokens
// / prompt_cache_miss_tokens (always present on providerMetadata, even when
// undefined — Go omits the key instead of including it with a nil value).
func deepseekPromptCacheTokens(raw json.RawMessage) (hit, miss *int) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var usage deepseekUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, nil
	}
	return usage.PromptCacheHitTokens, usage.PromptCacheMissTokens
}

func convertDeepseekUsage(raw json.RawMessage) types.Usage {
	if len(raw) == 0 || string(raw) == "null" {
		return types.Usage{}
	}

	var usage deepseekUsage
	_ = json.Unmarshal(raw, &usage)

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
		// Clamp at 0: a provider-reported reasoning token count that exceeds
		// completion_tokens must not produce a negative text token count.
		text := c - reasoning
		if text < 0 {
			text = 0
		}
		result.OutputDetails = &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning}
	}

	var rawMap map[string]interface{}
	_ = json.Unmarshal(raw, &rawMap)
	result.Raw = rawMap

	return result
}

type deepseekResponse struct {
	ID                string `json:"id"`
	Object            string `json:"object"`
	Created           int64  `json:"created"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint"`
	Choices           []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"` // thinking mode
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Logprobs *deepseekLogprobs `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
}

// deepseekLogprobEntry mirrors a single DeepSeek chat completion logprob
// entry (present on both non-streaming responses and stream chunks).
type deepseekLogprobEntry struct {
	Token       string  `json:"token"`
	Logprob     float64 `json:"logprob"`
	Bytes       []int   `json:"bytes"`
	TopLogprobs []struct {
		Token   string  `json:"token"`
		Logprob float64 `json:"logprob"`
		Bytes   []int   `json:"bytes"`
	} `json:"top_logprobs"`
}

// deepseekLogprobs mirrors DeepSeek's per-choice logprobs object.
type deepseekLogprobs struct {
	Content          []deepseekLogprobEntry `json:"content,omitempty"`
	ReasoningContent []deepseekLogprobEntry `json:"reasoning_content,omitempty"`
}

type deepseekUsage struct {
	PromptTokens          int  `json:"prompt_tokens"`
	CompletionTokens      int  `json:"completion_tokens"`
	TotalTokens           int  `json:"total_tokens"`
	PromptCacheHitTokens  *int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int `json:"prompt_cache_miss_tokens,omitempty"`
	PromptTokensDetails   *struct {
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

type deepseekStreamChunk struct {
	ID                string          `json:"id"`
	Object            string          `json:"object"`
	Created           int64           `json:"created"`
	Model             string          `json:"model"`
	SystemFingerprint string          `json:"system_fingerprint"`
	Error             json.RawMessage `json:"error,omitempty"`
	Choices           []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"` // thinking mode
			ToolCalls        []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Logprobs *deepseekLogprobs `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage,omitempty"`
}

type deepseekStream struct {
	reader              io.ReadCloser
	parser              *streaming.SSEParser
	err                 error
	toolCallTracker     *streaming.StreamingToolCallTracker
	flushQueue          []*provider.StreamChunk
	isActiveReasoning   bool
	includeRawChunks    bool
	responseHeaders     map[string]string
	metadataEmitted     bool
	providerOptionsName string

	// pendingFinish holds the Finish chunk built when finish_reason arrives.
	// Emission is deferred until the stream actually ends (the [DONE]
	// marker) so a trailing usage-only chunk (stream_options.include_usage)
	// can still be attached to it.
	pendingFinish *provider.StreamChunk

	// State accumulated across the stream for the Finish chunk's
	// providerMetadata and usage.
	usageRaw          json.RawMessage
	responseObject    string
	systemFingerprint string
	choiceIndex       *int
	messageRole       string
	toolCallTypes     map[int]string
	contentLogprobs   []deepseekLogprobEntry
	reasoningLogprobs []deepseekLogprobEntry
}

func newDeepseekStream(reader io.ReadCloser, includeRawChunks ...bool) *deepseekStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return &deepseekStream{
		reader:              reader,
		parser:              streaming.NewSSEParser(reader),
		toolCallTracker:     streaming.NewStreamingToolCallTracker(),
		includeRawChunks:    emitRaw,
		providerOptionsName: "deepseek",
		toolCallTypes:       map[int]string{},
	}
}

func (s *deepseekStream) Close() error { return s.reader.Close() }

func (s *deepseekStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, error) {
	if len(s.flushQueue) == 0 {
		return chunk, nil
	}
	s.flushQueue = append(s.flushQueue, chunk)
	return s.Next()
}

func (s *deepseekStream) Next() (*provider.StreamChunk, error) {
	if len(s.flushQueue) > 0 {
		chunk := s.flushQueue[0]
		s.flushQueue = s.flushQueue[1:]
		return chunk, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}
	if streaming.IsStreamDone(event) {
		if s.pendingFinish != nil {
			finish := s.pendingFinish
			s.pendingFinish = nil
			s.attachFinishMetadata(finish)
			s.err = io.EOF
			return finish, nil
		}
		s.err = io.EOF
		return nil, io.EOF
	}
	rawQueued := false
	if s.includeRawChunks {
		var raw interface{}
		if err := json.Unmarshal([]byte(event.Data), &raw); err != nil {
			raw = event.Data
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeRaw,
			Raw:  raw,
		})
		rawQueued = true
	}
	var chunkData deepseekStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		errorChunk := &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		}
		if rawQueued {
			s.flushQueue = append(s.flushQueue, errorChunk)
			return s.Next()
		}
		return errorChunk, nil
	}
	if len(chunkData.Error) > 0 {
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: deepseekStreamErrorText(chunkData.Error),
		})
	}
	if len(chunkData.Usage) > 0 && string(chunkData.Usage) != "null" {
		s.usageRaw = chunkData.Usage
	}
	if chunkData.Object != "" {
		s.responseObject = chunkData.Object
	}
	if chunkData.SystemFingerprint != "" {
		s.systemFingerprint = chunkData.SystemFingerprint
	}
	if !s.metadataEmitted && (chunkData.ID != "" || chunkData.Model != "" || chunkData.Created != 0 || len(s.responseHeaders) > 0) {
		metadata := &provider.ResponseMetadata{
			ID:      chunkData.ID,
			ModelID: chunkData.Model,
			Headers: s.responseHeaders,
		}
		if chunkData.Created != 0 {
			metadata.Timestamp = time.Unix(chunkData.Created, 0)
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: metadata,
		})
		s.metadataEmitted = true
	}
	if len(chunkData.Choices) > 0 {
		choice := chunkData.Choices[0]

		idx := choice.Index
		s.choiceIndex = &idx
		if choice.Delta.Role != "" {
			s.messageRole = choice.Delta.Role
		}
		if choice.Logprobs != nil {
			s.contentLogprobs = append(s.contentLogprobs, choice.Logprobs.Content...)
			s.reasoningLogprobs = append(s.reasoningLogprobs, choice.Logprobs.ReasoningContent...)
		}
		for _, tc := range choice.Delta.ToolCalls {
			if tc.Type != "" && tc.Index != nil {
				s.toolCallTypes[*tc.Index] = tc.Type
			}
		}

		// Handle reasoning content (emitted before text in DeepSeek thinking mode).
		if choice.Delta.ReasoningContent != "" {
			if !s.isActiveReasoning {
				s.isActiveReasoning = true
				s.flushQueue = append([]*provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
					{Type: provider.ChunkTypeReasoning, Reasoning: choice.Delta.ReasoningContent, ID: "reasoning-0"},
				}, s.flushQueue...)
				return s.Next()
			}
			return s.emitParsedChunk(&provider.StreamChunk{
				Type:      provider.ChunkTypeReasoning,
				Reasoning: choice.Delta.ReasoningContent,
				ID:        "reasoning-0",
			})
		}

		// End reasoning block when text content arrives.
		if choice.Delta.Content != "" {
			if s.isActiveReasoning {
				s.isActiveReasoning = false
				s.flushQueue = append([]*provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
					{Type: provider.ChunkTypeText, Text: choice.Delta.Content},
				}, s.flushQueue...)
				return s.Next()
			}
			return s.emitParsedChunk(&provider.StreamChunk{Type: provider.ChunkTypeText, Text: choice.Delta.Content})
		}
		// Tool call delta — accumulate partial arguments by index.
		// Finalize only when finish_reason is received, never mid-stream.
		if len(choice.Delta.ToolCalls) > 0 {
			for _, tc := range choice.Delta.ToolCalls {
				for _, chunk := range s.toolCallTracker.Track(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
					c := chunk
					s.flushQueue = append(s.flushQueue, &c)
				}
			}
			if choice.FinishReason != "" {
				s.flushDeepseekToolCalls(choice.FinishReason)
				return s.Next()
			}
			return s.Next()
		}
		if choice.FinishReason != "" {
			s.flushDeepseekToolCalls(choice.FinishReason)
			return s.Next()
		}
	}
	return s.Next()
}

func deepseekStreamErrorText(raw json.RawMessage) string {
	var withMessage struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &withMessage); err == nil && withMessage.Message != "" {
		return withMessage.Message
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func (s *deepseekStream) flushDeepseekToolCalls(finishReason string) {
	if s.isActiveReasoning {
		s.isActiveReasoning = false
		s.flushQueue = append([]*provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
		}, s.flushQueue...)
	}
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
	// Defer the Finish chunk itself: a stream_options.include_usage request
	// delivers usage in a trailing chunk after finish_reason, so the chunk is
	// only actually emitted once the stream ends (see the IsStreamDone
	// handling in Next()).
	s.pendingFinish = &provider.StreamChunk{
		Type:         provider.ChunkTypeFinish,
		FinishReason: providerutils.MapOpenAIFinishReason(finishReason),
	}
}

// attachFinishMetadata populates usage and providerMetadata on the deferred
// Finish chunk from state accumulated across the stream.
func (s *deepseekStream) attachFinishMetadata(chunk *provider.StreamChunk) {
	if len(s.usageRaw) > 0 {
		usage := convertDeepseekUsage(s.usageRaw)
		chunk.Usage = &usage
	}

	meta := map[string]interface{}{}
	if hitTokens, missTokens := deepseekPromptCacheTokens(s.usageRaw); hitTokens != nil || missTokens != nil {
		if hitTokens != nil {
			meta["promptCacheHitTokens"] = *hitTokens
		}
		if missTokens != nil {
			meta["promptCacheMissTokens"] = *missTokens
		}
	}
	if s.responseObject != "" {
		meta["responseObject"] = s.responseObject
	}
	if s.choiceIndex != nil {
		meta["choiceIndex"] = *s.choiceIndex
	}
	if s.messageRole != "" {
		meta["messageRole"] = s.messageRole
	}
	if len(s.toolCallTypes) > 0 {
		indices := make([]int, 0, len(s.toolCallTypes))
		for idx := range s.toolCallTypes {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		toolCallTypes := make([]string, 0, len(indices))
		for _, idx := range indices {
			toolCallTypes = append(toolCallTypes, s.toolCallTypes[idx])
		}
		meta["toolCallTypes"] = toolCallTypes
	}
	if len(s.contentLogprobs) > 0 || len(s.reasoningLogprobs) > 0 {
		lp := map[string]interface{}{}
		if len(s.contentLogprobs) > 0 {
			lp["content"] = s.contentLogprobs
		}
		if len(s.reasoningLogprobs) > 0 {
			lp["reasoning_content"] = s.reasoningLogprobs
		}
		meta["logprobs"] = lp
	}
	if s.systemFingerprint != "" {
		meta["systemFingerprint"] = s.systemFingerprint
	}
	if len(meta) == 0 {
		return
	}
	providerOptionsName := s.providerOptionsName
	if providerOptionsName == "" {
		providerOptionsName = "deepseek"
	}
	if raw, err := json.Marshal(map[string]interface{}{providerOptionsName: meta}); err == nil {
		chunk.ProviderMetadata = raw
	}
}

func (s *deepseekStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

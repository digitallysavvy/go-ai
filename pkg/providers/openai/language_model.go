package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// LanguageModel implements the provider.LanguageModel interface for OpenAI
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new OpenAI language model
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
	if m.provider.config.ChatProviderName != "" {
		return m.provider.config.ChatProviderName
	}
	if name := m.provider.Name(); name != "" && name != "openai" {
		return name + ".chat"
	}
	return "openai.chat"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Most OpenAI models support tools (gpt-4, gpt-3.5-turbo, etc.)
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Only vision models support images (gpt-4-vision, gpt-4-turbo, etc.)
	return m.modelID == "gpt-4-vision-preview" ||
		m.modelID == "gpt-4-turbo" ||
		m.modelID == "gpt-4o" ||
		m.modelID == "gpt-4o-mini"
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		return nil, err
	}

	// Make API request, capturing response headers.
	var response openAIResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/chat/completions",
		Body:    reqBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response to GenerateResult and attach HTTP headers.
	result := m.convertResponse(response)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	result.ResponseMetadata = &types.ResponseMetadata{
		ID:        response.ID,
		Timestamp: time.Unix(response.Created, 0).UTC(),
		ModelID:   response.Model,
		Headers:   result.ResponseHeaders,
	}
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming enabled
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts, true)
	if err != nil {
		return nil, err
	}

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/chat/completions",
		Body:    reqBody,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	inner := newOpenAIStreamWithMetadata(httpResp.Body, opts.IncludeRawChunks, m.Provider(), httpResp.Header)
	inner.requestBody = reqBody
	return streaming.NewWarningsStream(inner, warnings), nil
}

// buildRequestBody builds the OpenAI API request body.
//
// Deprecated: prefer buildRequestBodyWithWarnings, which also surfaces
// capability warnings (serviceTier, reasoningEffort, promptCacheRetention).
// Kept for existing callers/tests that only need the body.
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body, _, _ := m.buildRequestBodyWithWarnings(opts, stream)
	return body
}

// buildRequestBodyWithWarnings builds the OpenAI Chat Completions API request
// body plus any capability warnings (unsupported serviceTier, reasoningEffort,
// promptCacheRetention on GPT-6+, etc.), mirroring TS's getArgs(). It returns
// an error only for schema normalization failures that TS surfaces as a
// thrown UnsupportedFunctionalityError (d5e3024: a non-string propertyNames
// sub-schema).
func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	capabilities := GetLanguageModelCapabilities(m.modelID)
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
	}

	// Extract the "openai" provider options map once; every option below reads
	// from it.
	var openaiOpts map[string]interface{}
	if opts.ProviderOptions != nil {
		openaiOpts, _ = opts.ProviderOptions["openai"].(map[string]interface{})
	}

	// reasoningSummary is a Responses API option; the chat options schema has
	// no field for it, so it would otherwise be silently dropped. Warn
	// instead (TS commit 5b8e63bad8, #21178). The message is provider-neutral
	// because azure.chat() shares this code path.
	if v, ok := openaiOpts["reasoningSummary"]; ok && v != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoningSummary",
			Details: "reasoningSummary is only supported by the Responses API, not the Chat Completions API",
		})
	}

	// Extract store flag early — needed before message conversion so we can
	// filter unencrypted reasoning parts from assistant messages when store=false.
	// storeExplicit tracks whether the caller set the flag (so we only send it
	// in the request body when explicitly provided, not by default).
	store := true // effective value; default is server-side persistence
	storeExplicit := false
	if v, ok := openaiOpts["store"].(bool); ok {
		store = v
		storeExplicit = true
	}

	// isReasoningModel (TS getArgs: `openaiOptions.forceReasoning ??
	// modelCapabilities.isReasoningModel`). forceReasoning lets callers treat an
	// unrecognized "stealth" reasoning model ID as one, applying the same
	// parameter-compatibility rules and developer-role default.
	isReasoningModel := capabilities.IsReasoningModel
	if v, ok := openaiOpts["forceReasoning"].(bool); ok {
		isReasoningModel = v
	}

	// Resolve reasoning_effort: an explicit providerOptions.openai.reasoningEffort
	// string wins (TS getArgs: `openaiOptions.reasoningEffort ?? (isCustomReasoning(reasoning) ? reasoning : undefined)`).
	// types.ReasoningLevel's string values ("none", "minimal", "low", "medium",
	// "high", "xhigh") are the exact literals TS's top-level `reasoning` field
	// uses and forwards verbatim -- TS does NOT collapse "minimal" into "low" or
	// "xhigh" into "high"; it sends the level straight through as-is (isCustomReasoning
	// is true for anything except "provider-default"). Resolved early because the
	// reasoning-model parameter-compatibility gating below needs to know whether
	// the effective effort is "none" (TS getArgs).
	resolvedReasoningEffort := ""
	if v, ok := openaiOpts["reasoningEffort"].(string); ok && v != "" {
		resolvedReasoningEffort = v
	}
	if resolvedReasoningEffort == "" && opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		resolvedReasoningEffort = string(*opts.Reasoning)
	}

	// GPT-6+ models restrict reasoning effort to a fixed set (34c53c0); drop
	// and warn on anything outside it (matches TS getArgs()).
	if resolvedReasoningEffort != "" && capabilities.SupportedReasoningEfforts != nil &&
		!chatStringSliceContains(capabilities.SupportedReasoningEfforts, resolvedReasoningEffort) {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoningEffort",
			Details: m.modelID + " only supports the following reasoning efforts: " + strings.Join(capabilities.SupportedReasoningEfforts, ", "),
		})
		resolvedReasoningEffort = ""
	}

	// Convert messages, filtering unencrypted reasoning from assistant messages
	// when store=false. Without server-side persistence the API cannot reconstruct
	// the reasoning context in subsequent turns, making these parts useless.
	convertMessages := func(msgs []types.Message) []types.Message {
		if store {
			return msgs
		}
		filtered := make([]types.Message, len(msgs))
		copy(filtered, msgs)
		for i, msg := range filtered {
			if msg.Role == types.RoleAssistant {
				filtered[i].Content = filterUnencryptedReasoningParts(msg.Content, false)
			}
		}
		return filtered
	}

	// SanitizeReplayedToolCallArguments matches TS's OpenAI-only
	// serializeToolCallArguments (convert-to-openai-chat-messages.ts): a
	// replayed tool-call RawArguments string that doesn't parse to a JSON
	// object is sent as "{}" rather than forwarded verbatim. This is unique
	// to OpenAI's own chat conversion -- other ToOpenAIMessages callers
	// (Groq/DeepSeek/openai-compatible/Alibaba/...) must not opt in.
	toOpenAIMessagesOpts := prompt.ToOpenAIMessagesOptions{SanitizeReplayedToolCallArguments: true, IncludePromptCacheBreakpoint: true, AllowVideo: m.provider.config.AllowVideo}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(convertMessages(opts.Prompt.Messages), toOpenAIMessagesOpts)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text), toOpenAIMessagesOpts)
	}

	// Add system message if present.
	// Reasoning models (o1, o3, o4-mini, gpt-5.x non-chat, GPT-6+) require the
	// "developer" role instead of "system" per OpenAI's API specification
	// (34c53c0: GetLanguageModelCapabilities.SystemMessageMode). A caller may
	// override the mode explicitly via providerOptions.openai.systemMessageMode
	// (TS convertToOpenAIChatMessages: 'system' | 'developer' | 'remove'),
	// which takes precedence over the capability-derived default (TS getArgs:
	// `openaiOptions.systemMessageMode ?? (isReasoningModel ? 'developer' :
	// modelCapabilities.systemMessageMode)`).
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		role := capabilities.SystemMessageMode
		if isReasoningModel {
			role = "developer"
		}
		if v, ok := openaiOpts["systemMessageMode"].(string); ok && v != "" {
			role = v
		}
		switch role {
		case "remove":
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "system messages are removed for this model",
				Details: "system messages are removed for this model",
			})
			body["messages"] = messages
		default:
			systemMsg := map[string]interface{}{
				"role":    role,
				"content": opts.Prompt.System,
			}
			body["messages"] = append([]map[string]interface{}{systemMsg}, messages...)
		}
	}

	// Add optional parameters
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	if len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	// maxCompletionTokens (TS: openaiOptions.maxCompletionTokens, useful for
	// reasoning models). Forwarded unconditionally like TS baseArgs; the
	// reasoning-model gating below only backfills max_completion_tokens from
	// max_tokens when this was not already explicitly set.
	if v, ok := openaiOpts["maxCompletionTokens"]; ok && v != nil {
		if n, ok := intFromInterface(v); ok {
			body["max_completion_tokens"] = n
		}
	}
	// logitBias forwards as-is (TS: z.record(coerced numeric token id, bias)).
	if v, ok := openaiOpts["logitBias"]; ok && v != nil {
		body["logit_bias"] = v
	}
	// logprobs / top_logprobs (TS getArgs):
	//   logprobs: true when openaiOptions.logprobs === true or is a number.
	//   top_logprobs: the number when openaiOptions.logprobs is a number;
	//   0 when it is boolean true; omitted otherwise.
	if v, ok := openaiOpts["logprobs"]; ok && v != nil {
		switch lp := v.(type) {
		case bool:
			if lp {
				body["logprobs"] = true
				body["top_logprobs"] = 0
			}
		default:
			if n, ok := intFromInterface(v); ok {
				body["logprobs"] = true
				body["top_logprobs"] = n
			}
		}
	}
	if v, ok := openaiOpts["parallelToolCalls"].(bool); ok {
		body["parallel_tool_calls"] = v
	}
	if v, ok := openaiOpts["user"].(string); ok && v != "" {
		body["user"] = v
	}
	if v, ok := openaiOpts["metadata"]; ok && v != nil {
		body["metadata"] = v
	}
	if v, ok := openaiOpts["prediction"]; ok && v != nil {
		body["prediction"] = v
	}
	if v, ok := openaiOpts["safetyIdentifier"].(string); ok && v != "" {
		body["safety_identifier"] = v
	}
	if v, ok := openaiOpts["promptCacheKey"].(string); ok && v != "" {
		body["prompt_cache_key"] = v
	}

	// Remove unsupported sampling settings for reasoning models (TS getArgs;
	// see https://platform.openai.com/docs/guides/reasoning#limitations).
	// GPT-5.1+ (but not GPT-6+) models allow temperature/topP/logprobs when
	// reasoningEffort resolves to "none"
	// (GetLanguageModelCapabilities.SupportsNonReasoningParameters).
	if isReasoningModel {
		if resolvedReasoningEffort != "none" || !capabilities.SupportsNonReasoningParameters {
			if _, ok := body["temperature"]; ok {
				delete(body, "temperature")
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "temperature",
					Details: "temperature is not supported for reasoning models",
				})
			}
			if _, ok := body["top_p"]; ok {
				delete(body, "top_p")
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "topP",
					Details: "topP is not supported for reasoning models",
				})
			}
			if _, ok := body["logprobs"]; ok {
				delete(body, "logprobs")
				warnings = append(warnings, types.Warning{
					Type:    "other",
					Message: "logprobs is not supported for reasoning models",
					Details: "logprobs is not supported for reasoning models",
				})
			}
		}

		if _, ok := body["frequency_penalty"]; ok {
			delete(body, "frequency_penalty")
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "frequencyPenalty",
				Details: "frequencyPenalty is not supported for reasoning models",
			})
		}
		if _, ok := body["presence_penalty"]; ok {
			delete(body, "presence_penalty")
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "presencePenalty",
				Details: "presencePenalty is not supported for reasoning models",
			})
		}
		if _, ok := body["logit_bias"]; ok {
			delete(body, "logit_bias")
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "logitBias is not supported for reasoning models",
				Details: "logitBias is not supported for reasoning models",
			})
		}
		if _, ok := body["top_logprobs"]; ok {
			delete(body, "top_logprobs")
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "topLogprobs is not supported for reasoning models",
				Details: "topLogprobs is not supported for reasoning models",
			})
		}

		// Reasoning models use max_completion_tokens instead of max_tokens.
		if maxTokens, ok := body["max_tokens"]; ok {
			if _, hasMCT := body["max_completion_tokens"]; !hasMCT {
				body["max_completion_tokens"] = maxTokens
			}
			delete(body, "max_tokens")
		}
	} else if strings.HasPrefix(m.modelID, "gpt-4o-search-preview") ||
		strings.HasPrefix(m.modelID, "gpt-4o-mini-search-preview") {
		if _, ok := body["temperature"]; ok {
			delete(body, "temperature")
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "temperature",
				Details: "temperature is not supported for the search preview models and has been removed.",
			})
		}
	}

	// Add tools if present. Function tool parameter schemas are normalized
	// for OpenAI structured outputs (d5e3024, 411b3f2), mirroring TS
	// prepareChatTools -- kept local to the OpenAI chat model rather than the
	// shared tool.ToOpenAIFormat helper, which other OpenAI-compatible
	// providers also use.
	if len(opts.Tools) > 0 {
		openaiTools := tool.ToOpenAIFormat(opts.Tools)
		normalizedTools, toolSchemaWarnings, normErr := normalizeOpenAIChatToolSchemas(openaiTools)
		if normErr != nil {
			return nil, nil, normErr
		}
		warnings = append(warnings, toolSchemaWarnings...)
		body["tools"] = normalizedTools
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}

	// Add response format if present
	// Response format (TS openai-chat-language-model.ts): json_schema when a
	// schema is present (strictJsonSchema defaults to true), else json_object.
	// The schema is normalized for OpenAI structured outputs first (d5e3024,
	// 411b3f2: drop propertyNames / lookaround patterns).
	responseFormat := opts.ResponseFormat
	if responseFormat != nil && responseFormat.Type == "json" {
		if rawSchema := providerutils.ResponseFormatJSONSchema(responseFormat.Schema); rawSchema != nil {
			if schemaMap, ok := rawSchema.(map[string]interface{}); ok {
				normalizedSchema, schemaWarnings, normErr := NormalizeOpenAIJSONSchema(schemaMap)
				if normErr != nil {
					return nil, nil, normErr
				}
				warnings = append(warnings, schemaWarnings...)
				rfCopy := *responseFormat
				rfCopy.Schema = normalizedSchema
				responseFormat = &rfCopy
			}
		}
	}
	if format, fmtWarnings := providerutils.ChatResponseFormat(responseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs: true,
		StrictJSONSchema:  m.strictJSONSchema(opts.ProviderOptions),
	}); format != nil {
		body["response_format"] = format
		warnings = append(warnings, fmtWarnings...)
	}

	if resolvedReasoningEffort != "" {
		body["reasoning_effort"] = resolvedReasoningEffort
	}

	// Apply OpenAI-specific provider options
	// Add prompt cache retention if present.
	// Supports "in_memory" (default) and "24h" (for gpt-5.1 series).
	// GPT-6+ models don't support it; use promptCacheOptions instead
	// (b2b1bb9).
	if promptCacheRetention, ok := openaiOpts["promptCacheRetention"].(string); ok && promptCacheRetention != "" {
		if capabilities.SupportedReasoningEfforts != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "promptCacheRetention",
				Details: "promptCacheRetention is not supported by GPT-6 and later models; use promptCacheOptions instead",
			})
		} else {
			body["prompt_cache_retention"] = promptCacheRetention
		}
	}
	// promptCacheOptions (b2b1bb9): {mode, ttl} forwarded as-is to
	// prompt_cache_options.
	if promptCacheOptions, ok := openaiOpts["promptCacheOptions"]; ok && promptCacheOptions != nil {
		body["prompt_cache_options"] = promptCacheOptions
	}
	// serviceTier (17d3436/4cd4548): flex requires supportsFlexProcessing;
	// priority/fast require supportsPriorityProcessing.
	if serviceTier, ok := openaiOpts["serviceTier"].(string); ok && serviceTier != "" {
		switch {
		case serviceTier == "flex" && !capabilities.SupportsFlexProcessing:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "serviceTier",
				Details: "flex processing is only available for o3, o4-mini, and gpt-5 models",
			})
		case (serviceTier == "priority" || serviceTier == "fast") && !capabilities.SupportsPriorityProcessing:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "serviceTier",
				Details: "priority processing is only available for supported models (gpt-4, gpt-5, gpt-5-mini, o3, o4-mini) and requires Enterprise access. gpt-5-nano is not supported",
			})
		default:
			body["service_tier"] = serviceTier
		}
	}
	// Forward store only when explicitly set (already extracted above).
	if storeExplicit {
		body["store"] = store
	}
	// textVerbosity controls the length/detail of the model's text response.
	// Maps to top-level "verbosity" in the Chat Completions API.
	if v, ok := openaiOpts["textVerbosity"].(string); ok {
		body["verbosity"] = v
	}

	if m.provider.config.TransformRequestBody != nil {
		body = m.provider.config.TransformRequestBody(body)
	}

	return body, warnings, nil
}

func chatStringSliceContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// convertResponse converts an OpenAI response to GenerateResult
// Updated in v6.0 to support detailed usage tracking
func (m *LanguageModel) convertResponse(response openAIResponse) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:       convertOpenAIUsage(response.Usage),
		RawResponse: response,
	}

	// Extract content from first choice
	if len(response.Choices) > 0 {
		choice := response.Choices[0]

		// Extract text: prefer message content, falling back to the audio
		// transcript for audio-output models (6d1f881).
		if choice.Message.Content != "" {
			result.Text = choice.Message.Content
		} else if choice.Message.Audio != nil && choice.Message.Audio.Transcript != "" {
			result.Text = choice.Message.Audio.Transcript
		}

		// Extract tool calls
		if len(choice.Message.ToolCalls) > 0 {
			result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
			for i, tc := range choice.Message.ToolCalls {
				var args map[string]interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck

				result.ToolCalls[i] = types.ToolCall{
					ID:        tc.ID,
					ToolName:  tc.Function.Name,
					Arguments: args,
				}
			}
		}

		// Extract finish reason
		result.FinishReason = providerutils.MapOpenAIFinishReason(choice.FinishReason)
		result.RawFinishReason = choice.FinishReason
	}

	return result
}

// convertOpenAIUsage converts OpenAI usage to detailed Usage struct
// Implements the v6.0 detailed token tracking with cache and reasoning tokens
func convertOpenAIUsage(usage openAIUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)

	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}

	// Calculate cached tokens (cache read)
	var cachedTokens int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cachedTokens = int64(*usage.PromptTokensDetails.CachedTokens)
	}

	// Cache write tokens (b2b1bb9, GPT-5.6+ prompt caching).
	var cacheWriteTokens *int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CacheWriteTokens != nil {
		v := int64(*usage.PromptTokensDetails.CacheWriteTokens)
		cacheWriteTokens = &v
	}

	// Extract text and image tokens
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

	// Calculate reasoning tokens
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}

	// Set input token details
	if cachedTokens > 0 || cacheWriteTokens != nil || textTokens != nil || imageTokens != nil {
		noCacheTokens := promptTokens - cachedTokens
		if cacheWriteTokens != nil {
			noCacheTokens -= *cacheWriteTokens
		}
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedTokens,
			CacheWriteTokens: cacheWriteTokens,
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
		}
	}

	// Set output token details
	if reasoningTokens > 0 {
		textTokens := completionTokens - reasoningTokens
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		}
	}

	// Store raw usage for provider-specific details
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

// handleError converts various errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	// Try to parse as OpenAI error response
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

// openAIResponse represents the OpenAI API response
// Updated in v6.0 to support detailed token usage
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage openAIUsage `json:"usage"`
}

// openAIUsage represents OpenAI usage information with detailed token tracking
type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Detailed token breakdown (v6.0)
	PromptTokensDetails *struct {
		CachedTokens     *int `json:"cached_tokens,omitempty"`
		CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
		AudioTokens      *int `json:"audio_tokens,omitempty"`
		TextTokens       *int `json:"text_tokens,omitempty"`
		ImageTokens      *int `json:"image_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`

	CompletionTokensDetails *struct {
		ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

// openAIMessage represents an OpenAI message
type openAIMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
	// Audio carries the transcript for audio-output chat completions
	// (gpt-4o-audio-preview etc.); used as fallback text when Content is
	// empty (6d1f881).
	Audio *struct {
		Transcript string `json:"transcript"`
	} `json:"audio,omitempty"`
}

// openAIToolCall represents an OpenAI tool call
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

// filterUnencryptedReasoningParts removes ReasoningContent parts that have no
// EncryptedContent when store=false. Without server-side storage the API cannot
// reconstruct the reasoning context in subsequent turns, so these parts are
// useless and should be dropped from the returned content.
//
// When store is true (or the store flag is not set), the slice is returned unchanged.
func filterUnencryptedReasoningParts(content []types.ContentPart, store bool) []types.ContentPart {
	if store {
		return content
	}
	result := make([]types.ContentPart, 0, len(content))
	for _, part := range content {
		if rc, ok := part.(types.ReasoningContent); ok {
			if rc.EncryptedContent == "" {
				// Drop: no encrypted content, cannot be replayed without storage.
				continue
			}
		}
		result = append(result, part)
	}
	return result
}

// isReasoningModel reports whether modelID is a reasoning model that requires
// the "developer" role for system messages instead of "system".
//
// Delegates to GetLanguageModelCapabilities (34c53c0), which ports
// TypeScript's regex-based GPT/o-series version parsing (correctly handling
// GPT-6+ and GPT-5.6 models) instead of the previous ad-hoc prefix list.
func isReasoningModel(modelID string) bool {
	return GetLanguageModelCapabilities(modelID).IsReasoningModel
}

// supportsNonReasoningParameters reports whether a reasoning model accepts
// standard sampling parameters when reasoning effort is disabled.
//
// Delegates to GetLanguageModelCapabilities (34c53c0).
func supportsNonReasoningParameters(modelID string) bool {
	return GetLanguageModelCapabilities(modelID).SupportsNonReasoningParameters
}

// openAIStream implements provider.TextStream for OpenAI streaming
type openAIStream struct {
	reader           io.ReadCloser
	parser           *streaming.SSEParser
	err              error
	toolCallTracker  *streaming.StreamingToolCallTracker
	flushQueue       []*provider.StreamChunk // fully assembled chunks ready to emit
	includeRawChunks bool
	metadataEmitted  bool
	pendingRaw       []*provider.StreamChunk
	pendingMetadata  *provider.StreamChunk
	outputStarted    bool
	providerName     string
	responseHeaders  http.Header
	requestBody      interface{}
}

// newOpenAIStream creates a new OpenAI stream
func newOpenAIStream(reader io.ReadCloser, includeRawChunks ...bool) *openAIStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return newOpenAIStreamWithMetadata(reader, emitRaw, "openai.chat", nil)
}

func newOpenAIStreamWithMetadata(reader io.ReadCloser, emitRaw bool, providerName string, headers http.Header) *openAIStream {
	if providerName == "" {
		providerName = "openai.chat"
	}
	return &openAIStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		toolCallTracker:  streaming.NewStreamingToolCallTracker(),
		includeRawChunks: emitRaw,
		providerName:     providerName,
		responseHeaders:  headers,
	}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that opened this stream in types.StepRequest.Body for
// streaming calls, matching TS doStream()'s {request: {body}} (hand-off:
// "stream request body field").
func (s *openAIStream) RequestBody() interface{} {
	return s.requestBody
}

// Close implements io.Closer
func (s *openAIStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *openAIStream) Next() (*provider.StreamChunk, error) {
	for {
		// Emit any fully-assembled chunks before reading more SSE events.
		if len(s.flushQueue) > 0 {
			chunk := s.flushQueue[0]
			s.flushQueue = s.flushQueue[1:]
			return chunk, nil
		}

		if s.err != nil {
			return nil, s.err
		}

		// Get next SSE event
		event, err := s.parser.Next()
		if err != nil {
			s.err = err
			return nil, err
		}

		// Check for stream completion
		if streaming.IsStreamDone(event) {
			s.err = io.EOF
			return nil, io.EOF
		}
		// Parse the event data as JSON.
		// Tool call deltas carry an "index" field not present in non-streaming responses,
		// so we use an inline struct here instead of the shared openAIToolCall type.
		var chunkData struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    *int    `json:"index"`
						ID       string  `json:"id"`
						Type     *string `json:"type"` // nullable: OpenAI may send null for type in streaming deltas (#12901)
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error json.RawMessage `json:"error,omitempty"`
		}

		if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
			s.queueRawChunk(event.Data)
			errorChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, errorChunk)
				continue
			}
			return errorChunk, nil
		}
		rawChunk := s.rawChunk(event.Data)
		if len(chunkData.Error) > 0 {
			if !s.outputStarted {
				s.flushQueue = nil
				s.pendingRaw = nil
				s.pendingMetadata = nil
				s.err = newOpenAIStreamProviderError(s.providerName, json.RawMessage(event.Data), s.responseHeaders)
				return nil, s.err
			}
			if rawChunk != nil {
				s.flushQueue = append(s.flushQueue, rawChunk)
			}
			errorChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: openAIStreamErrorText(chunkData.Error),
				// P1-1c part 2: attach a structured StreamProviderError (mirrors
				// TS createOpenAIProviderStreamError) so streamRetries/IsRetryable
				// see the real type/code/statusCode/isRetryable instead of
				// falling back to generic text-based inference.
				Err: newOpenAIStreamProviderErrorChunk(s.providerName, chunkData.Error),
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, errorChunk)
				continue
			}
			return errorChunk, nil
		}

		if !s.metadataEmitted && (chunkData.ID != "" || chunkData.Model != "" || chunkData.Created != 0) {
			metadata := &provider.ResponseMetadata{
				ID:      chunkData.ID,
				ModelID: chunkData.Model,
			}
			if chunkData.Created != 0 {
				metadata.Timestamp = time.Unix(chunkData.Created, 0)
			}
			s.pendingMetadata = &provider.StreamChunk{
				Type:             provider.ChunkTypeResponseMetadata,
				ResponseMetadata: metadata,
			}
			s.metadataEmitted = true
		}

		if len(chunkData.Choices) > 0 {
			choice := chunkData.Choices[0]

			// Text chunk
			if choice.Delta.Content != "" {
				s.outputStarted = true
				if rawChunk != nil {
					s.pendingRaw = append(s.pendingRaw, rawChunk)
				}
				s.flushPendingMetadata()
				textChunk := &provider.StreamChunk{
					Type: provider.ChunkTypeText,
					Text: choice.Delta.Content,
				}
				if len(s.flushQueue) > 0 {
					s.flushQueue = append(s.flushQueue, textChunk)
					continue
				}
				return textChunk, nil
			}

			// Tool call delta — accumulate partial arguments by index.
			// OpenAI sends: first delta has id + name + empty/partial args;
			// subsequent deltas for the same index carry argument fragments only.
			if len(choice.Delta.ToolCalls) > 0 {
				if rawChunk != nil {
					s.pendingRaw = append(s.pendingRaw, rawChunk)
				}
				for _, tc := range choice.Delta.ToolCalls {
					for _, chunk := range s.toolCallTracker.Track(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
						c := chunk
						s.flushPendingMetadata()
						s.flushQueue = append(s.flushQueue, &c)
						s.outputStarted = true
					}
				}
				if choice.FinishReason != nil {
					s.flushOpenAIToolCalls(*choice.FinishReason)
				}
				// No chunk to emit yet — keep accumulating.
				continue
			}

			// Finish chunk — flush all accumulated tool calls first.
			if choice.FinishReason != nil {
				if rawChunk != nil && !s.outputStarted {
					s.pendingRaw = append(s.pendingRaw, rawChunk)
				}
				s.flushPendingMetadata()
				s.flushOpenAIToolCalls(*choice.FinishReason)
				s.outputStarted = true
				continue
			}
		}

		if rawChunk != nil {
			s.pendingRaw = append(s.pendingRaw, rawChunk)
		}
		// Empty chunk, get next
		continue

	}
}

func (s *openAIStream) flushPendingMetadata() {
	if len(s.pendingRaw) > 0 {
		s.flushQueue = append(s.flushQueue, s.pendingRaw...)
		s.pendingRaw = nil
	}
	if s.pendingMetadata != nil {
		s.flushQueue = append(s.flushQueue, s.pendingMetadata)
		s.pendingMetadata = nil
	}
}

func (s *openAIStream) queueRawChunk(data string) {
	if raw := s.rawChunk(data); raw != nil {
		s.flushQueue = append(s.flushQueue, raw)
	}
}

func (s *openAIStream) rawChunk(data string) *provider.StreamChunk {
	if !s.includeRawChunks {
		return nil
	}
	var raw interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		raw = data
	}
	return &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw}
}

func openAIStreamErrorText(raw json.RawMessage) string {
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

// Err returns any error that occurred during streaming
func (s *openAIStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *openAIStream) flushOpenAIToolCalls(finishReason string) {
	// Emit one ChunkTypeToolCall per accumulated entry in index order.
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
		Type:            provider.ChunkTypeFinish,
		FinishReason:    providerutils.MapOpenAIFinishReason(finishReason),
		RawFinishReason: finishReason,
	})
}

// strictJSONSchema reads the strictJsonSchema provider option (default true).
// OpenAI-compatible hosts built on this model (e.g. Cerebras, Baseten,
// DeepInfra) read it from their own provider options key, like the TS
// openai-compatible chat model; the "openai" key is always honoured.
func (m *LanguageModel) strictJSONSchema(providerOptions map[string]interface{}) bool {
	strict := true
	for _, key := range []string{"openai", m.provider.Name(), providerutils.ToOpenAICompatibleCamelCase(m.provider.Name())} {
		if opts, ok := providerOptions[key].(map[string]interface{}); ok {
			if v, ok := opts["strictJsonSchema"].(bool); ok {
				strict = v
			}
		}
	}
	return strict
}

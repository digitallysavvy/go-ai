package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
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
	return "v4"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "fireworks.chat"
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
	reqBody, warnings, err := m.PrepareRequest(ctx, opts)
	if err != nil {
		return nil, err
	}
	var response fireworksResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:           http.MethodPost,
		Path:             "/v1/chat/completions",
		Body:             reqBody,
		Headers:          opts.Headers,
		ValidateResponse: providerutils.ChatResponseValidator(m.Provider(), providerutils.ResponseKindOpenAICompatible),
	}, &streaming.OpenAICompatChatDecoder{Target: &response})
	if err != nil {
		return nil, m.handleError(err)
	}
	result, err := m.convertGenerateResponse(response, resp.Body, providerutils.ExtractHeaders(resp.Headers))
	if err != nil {
		return nil, err
	}
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

// PrepareRequest returns the Chat Completions body and warnings DoGenerate
// would send, without a network call and without stream fields (TS
// OpenAICompatibleChatLanguageModel.prepareRequest, c9c6402cbd). Call
// headers are not applied.
func (m *LanguageModel) PrepareRequest(ctx context.Context, opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}
	if err := providerutils.ValidateOpenAICompatibleChatOptions(opts.ProviderOptions, "fireworks"); err != nil {
		return nil, nil, err
	}
	if err := prompt.ValidateOpenAIFileParts(opts.Prompt.Messages, prompt.FilePartsOpenAICompatible, "fireworks"); err != nil {
		return nil, nil, err
	}
	// TS openai-compatible prepareTools throws for an unknown tool choice type.
	if err := prompt.ValidateToolChoiceType(opts.Tools, opts.ToolChoice); err != nil {
		return nil, nil, err
	}
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts, false)
	return reqBody, warnings, nil
}

// ConvertResponse converts a Chat Completions response body (JSON) into a
// generate result, without a network call (TS
// OpenAICompatibleChatLanguageModel.convertResponse, c9c6402cbd): content,
// finish reason, usage, provider metadata and the response id, model id and
// timestamp. The result has no warnings, request, response headers or
// response body. The provider metadata key is "fireworks" (its raw and
// camelCase provider options names are the same, so providerOptions does not
// change it). A body that fails the response schema returns a
// *providererrors.ValidationError, one without choices an
// InvalidResponseDataError.
func (m *LanguageModel) ConvertResponse(ctx context.Context, body []byte, providerOptions map[string]interface{}) (*types.GenerateResult, error) {
	if err := streaming.ValidateChatResponseBody(body, providerutils.ResponseKindOpenAICompatible); err != nil {
		return nil, err
	}
	var response fireworksResponse
	if err := streaming.DecodeChatResponseBody(body, &response); err != nil {
		_, typed := providerutils.ChunkParseFailure(string(body), err)
		return nil, typed
	}
	result, err := m.convertGenerateResponse(response, body, nil)
	if err != nil {
		return nil, err
	}
	result.RawResponse = nil
	result.Warnings = []types.Warning{}
	return result, nil
}

// convertGenerateResponse is the response conversion DoGenerate and
// ConvertResponse share (TS convertGenerateResponse).
func (m *LanguageModel) convertGenerateResponse(response fireworksResponse, body []byte, headers map[string]string) (*types.GenerateResult, error) {
	result := m.convertResponse(response)
	if err := streaming.ApplyOpenAICompatibleChatResponse(result, body, headers, "fireworks"); err != nil {
		return nil, err
	}
	providerutils.ApplyWireUsageRaw(result, body)
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if err := providerutils.ValidateOpenAICompatibleChatOptions(opts.ProviderOptions, "fireworks"); err != nil {
		return nil, err
	}
	if err := prompt.ValidateOpenAIFileParts(opts.Prompt.Messages, prompt.FilePartsOpenAICompatible, "fireworks"); err != nil {
		return nil, err
	}
	// TS openai-compatible prepareTools throws for an unknown tool choice type.
	if err := prompt.ValidateToolChoiceType(opts.Tools, opts.ToolChoice); err != nil {
		return nil, err
	}
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts, true)
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Body:    reqBody,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	stream := newFireworksStream(httpResp.Body)
	stream.SetRequestBody(reqBody)
	stream.IncludeRawChunks = opts.IncludeRawChunks
	stream.HTTPResponseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return streaming.NewWarningsStream(stream, warnings), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body, _ := m.buildRequestBodyWithWarnings(opts, stream)
	return body
}

func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning) {
	var warnings []types.Warning
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
	toOpenAIMessagesOpts := prompt.ToOpenAIMessagesOptions{AllowVideo: true, Provider: "fireworks", ThoughtSignatureKey: "fireworks", ReasoningKey: "reasoning_content", SpreadCompatMetadata: true, FileParts: prompt.FilePartsOpenAICompatible}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages, toOpenAIMessagesOpts)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text), toOpenAIMessagesOpts)
	}
	if opts.Prompt.System != "" {
		// A System-only prompt has no messages yet (TS sends [{role: system}]).
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
	var toolWarnings []types.Warning
	if len(opts.Tools) > 0 {
		functionTools, dropped := tool.DropProviderDefinedTools(opts.Tools, nil)
		toolWarnings = dropped
		body["tools"] = tool.ToOpenAIFormat(functionTools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	// Response format (TS openai-compatible chat model with
	// supportsStructuredOutputs: true): json_schema when a schema is present,
	// strictJsonSchema (default true) from the fireworks provider options.
	fireworksOptions, optionWarnings := providerutils.ResolveOpenAICompatibleProviderOptions("fireworks", opts.ProviderOptions)
	warnings = append(warnings, optionWarnings...)
	warnings = append(warnings, providerutils.OpenAICompatibleCommonOptionWarnings(fireworksOptions)...)
	// Fireworks' transformRequestBody consumes thinking, reasoningHistory,
	// promptCacheKey and serviceTier itself; every other unknown provider
	// option is spread into the body like the shared TS chat model does.
	warnings = append(warnings, providerutils.ApplyOpenAICompatibleCallSettings(body, opts.FrequencyPenalty, opts.PresencePenalty, opts.Seed, opts.TopK, "fireworks", opts.ProviderOptions,
		"thinking", "reasoningHistory", "promptCacheKey", "serviceTier")...)
	if user, ok := providerutils.OpenAICompatibleStringOption(fireworksOptions, "user"); ok {
		body["user"] = user
	}
	if verbosity, ok := providerutils.OpenAICompatibleStringOption(fireworksOptions, "textVerbosity", "text_verbosity", "text-verbosity"); ok {
		body["verbosity"] = verbosity
	}
	if format, _ := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs: true,
		StrictJSONSchema:  providerutils.BoolOption(fireworksOptions, "strictJsonSchema", true),
	}); format != nil {
		body["response_format"] = format
	}
	// reasoning_effort (TS 61a315e333, 5006d83c4e): the provider option is
	// sent unchanged (an explicit null as null); otherwise the top-level
	// reasoning goes through Fireworks' mapReasoning, which sends every level
	// except those the model family rejects ("none" for gpt-oss / glm-5p3 /
	// muse-spark, "minimal" for nemotron / inkling), rounded with a
	// compatibility warning.
	if reasoningEffort, ok := providerutils.ResolveOpenAICompatibleReasoningEffort(fireworksOptions, opts.Reasoning, func(reasoning *types.ReasoningLevel) (string, bool) {
		return providerutils.MapReasoningToProviderEffort(reasoning, fireworksReasoningEffortMap(m.modelID), nil, &warnings)
	}); ok {
		body["reasoning_effort"] = reasoningEffort
	}

	// Fireworks-specific options (providerOptions.fireworks; TS
	// fireworksLanguageModelOptions/fireworks-provider.ts's
	// transformRequestBody). All of these previously read from
	// opts.ProviderOptions[...] directly (the top-level namespace) instead of
	// the "fireworks" provider-options namespace resolved above, so they were
	// silently ignored whenever a caller correctly namespaced them under
	// providerOptions.fireworks.
	// TS transformRequestBody copies the values as given
	// (fireworks-provider.ts:177): promptCacheKey / serviceTier whenever the
	// key is present (!== undefined, null included), thinking as
	// {type, budget_tokens} for any truthy value (a non-object gives {}),
	// reasoningHistory when truthy.
	if thinking, present := fireworksOptions["thinking"]; present && providerutils.JSTruthy(thinking) {
		thinkingBody := make(map[string]interface{})
		if t, ok := thinking.(map[string]interface{}); ok {
			if thinkingType, has := t["type"]; has {
				thinkingBody["type"] = thinkingType
			}
			if budgetTokens, has := t["budgetTokens"]; has {
				thinkingBody["budget_tokens"] = budgetTokens
			}
		}
		body["thinking"] = thinkingBody
	}
	if reasoningHistory, present := fireworksOptions["reasoningHistory"]; present && providerutils.JSTruthy(reasoningHistory) {
		body["reasoning_history"] = reasoningHistory
	}
	if promptCacheKey, present := fireworksOptions["promptCacheKey"]; present {
		body["prompt_cache_key"] = promptCacheKey
	}
	if serviceTier, present := fireworksOptions["serviceTier"]; present {
		body["service_tier"] = serviceTier
	}

	return body, append(warnings, toolWarnings...)
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
		Usage:           convertFireworksUsagePtr(response.Usage),
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
			// TS openai-compatible chat: `toolCall.id || generateId()`.
			id := tc.ID
			if id == "" {
				id = streaming.GenerateID()
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:        id,
				ToolName:  tc.Function.Name,
				Arguments: args,
				// Wire text keeps the key order TS keeps (JSON.stringify of the parsed input).
				RawArguments: tc.Function.Arguments,
			}
		}
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure
	}
	if parsed := parseFireworksProviderError(err); parsed != nil {
		return parsed
	}
	// TS createJsonErrorResponseHandler always sets statusCode: response.status
	// even in its catch-all branch; preserve the real HTTP status here too
	// instead of hardcoding 0 (which several retry-classification paths treat
	// as "unknown", i.e. retryable).
	message := err.Error()
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		statusCode := statusErr.StatusCode
		// TS createJsonErrorResponseHandler: an empty body, or one that does
		// not parse (secureJsonParse) or match the error schema, gives the
		// response status text.
		if text := http.StatusText(statusCode); text != "" {
			message = text
		}
	}
	// The status code, response headers, body and data come from the HTTP
	// cause (TS APICallError), so retries honor retry-after headers.
	pe := providererrors.NewProviderErrorFromResponse("fireworks", 0, "", message, err)
	pe.Data = nil // TS: no data for a body that does not match the error schema
	return pe
}

// convertFireworksUsage converts Fireworks usage to detailed Usage struct
// convertFireworksUsagePtr is TS convert usage of a possibly missing wire usage: an
// absent or null usage object yields the null usage (unknown token counts),
// not zero tokens.
func convertFireworksUsagePtr(usage *fireworksUsage) types.Usage {
	if usage == nil {
		return types.Usage{}
	}
	return convertFireworksUsage(*usage)
}

func convertFireworksUsage(usage fireworksUsage) types.Usage {
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
	{
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedTokens,
			CacheWriteTokens: nil,
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
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

type fireworksResponse struct {
	ID      string  `json:"id"`
	Object  string  `json:"object"`
	Created float64 `json:"created"` // TS z.number()
	Model   string  `json:"model"`
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
	Usage *fireworksUsage `json:"usage"`
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
	s.MetadataKey = "fireworks"
	s.ChunkErrorKind = providerutils.ChunkErrorStringOrObject
	s.OnReasoningDelta = func(eventBytes []byte) (string, bool) {
		var chunk struct {
			Choices []struct {
				Delta struct {
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := jsonexact.Unmarshal(eventBytes, &chunk); err != nil || len(chunk.Choices) == 0 {
			return "", false
		}
		rc := chunk.Choices[0].Delta.ReasoningContent
		return rc, rc != ""
	}
	return &fireworksStream{OpenAICompatStream: s}
}

// fireworksModelsWithoutNoneReasoningEffort are the model families that
// always reason and reject reasoning effort "none" (TS fireworks-provider.ts).
var fireworksModelsWithoutNoneReasoningEffort = regexp.MustCompile(`^(?:gpt-oss|glm-5p3|muse-spark)`)

// fireworksModelsWithoutMinimalReasoningEffort reject "minimal".
var fireworksModelsWithoutMinimalReasoningEffort = regexp.MustCompile(`^(?:nemotron|inkling)`)

// fireworksReasoningEffortMap ports TS getFireworksReasoningEffortMap: every
// level is sent as is, except "none" and "minimal" for the model families
// known to reject them (matched on the name after the last "/").
func fireworksReasoningEffortMap(modelID string) map[types.ReasoningLevel]string {
	modelName := modelID[strings.LastIndex(modelID, "/")+1:]
	effortMap := map[types.ReasoningLevel]string{
		types.ReasoningLow: "low", types.ReasoningMedium: "medium", types.ReasoningHigh: "high",
		types.ReasoningXHigh: "xhigh", types.ReasoningMax: "max",
	}
	if !fireworksModelsWithoutNoneReasoningEffort.MatchString(modelName) {
		effortMap[types.ReasoningNone] = "none"
	}
	if !fireworksModelsWithoutMinimalReasoningEffort.MatchString(modelName) {
		effortMap[types.ReasoningMinimal] = "minimal"
	}
	return effortMap
}

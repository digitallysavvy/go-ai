package moonshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// LanguageModel implements the provider.LanguageModel interface for Moonshot models
type LanguageModel struct {
	prov    *Provider
	modelID string
}

// NewLanguageModel creates a new Moonshot language model
func NewLanguageModel(prov *Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		prov:    prov,
		modelID: modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "moonshot"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return true // Moonshot models support OpenAI-compatible tool calling
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true // all models support at least json_object fallback
}

// SupportsImageInput returns whether the model accepts image inputs.
// Moonshot Chat Completions accepts image_url/video_url content parts on any
// model (no model-specific gating in the message converter); whether a given
// model actually attends to the image is a model-serving concern.
func (m *LanguageModel) SupportsImageInput() bool {
	return true
}

// SupportedURLs reports that Moonshot natively accepts ms:// file references
// (from the Moonshot Files API) as image/video URLs without the SDK
// downloading and inlining them first. Mirrors TS supportedUrls.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*": {`^ms://`},
		"video/*": {`^ms://`},
	}
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		return nil, err
	}

	var response moonshotResponse
	resp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/chat/completions",
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

	httpResp, err := m.prov.client.DoStream(ctx, internalhttp.Request{
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

	inner := newMoonshotStream(httpResp.Body, opts.IncludeRawChunks)
	return providerutils.WithResponseMetadata(streaming.NewWarningsStream(inner, warnings), httpResp.Header, m.ModelID()), nil
}

// buildRequestBody builds the API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body, _, _ := m.buildRequestBodyWithWarnings(opts, stream)
	return body
}

// buildRequestBodyWithWarnings builds the Moonshot Chat Completions request
// body. Mirrors TS MoonshotAIChatLanguageModel.getArgs.
func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	moonshotOpts, resolveWarnings := providerutils.ResolveOpenAICompatibleProviderOptions("moonshot", opts.ProviderOptions)
	mergeMoonshotAIProviderOptions(moonshotOpts, opts.ProviderOptions)
	modelOpts, err := parseMoonshotModelOptions(moonshotOpts)
	if err != nil {
		return nil, nil, err
	}

	var warnings []types.Warning
	warnings = append(warnings, resolveWarnings...)

	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}

	supportsSampling := !IsKimiModel(m.modelID)
	if !supportsSampling && opts.Temperature != nil {
		warnings = append(warnings, moonshotFixedSamplingWarning("temperature", m.modelID))
	}
	if !supportsSampling && opts.TopP != nil {
		warnings = append(warnings, moonshotFixedSamplingWarning("topP", m.modelID))
	}
	if !supportsSampling && opts.FrequencyPenalty != nil {
		warnings = append(warnings, moonshotFixedSamplingWarning("frequencyPenalty", m.modelID))
	}
	if !supportsSampling && opts.PresencePenalty != nil {
		warnings = append(warnings, moonshotFixedSamplingWarning("presencePenalty", m.modelID))
	}

	moonshotTools, moonshotToolChoice, toolWarnings, err := prepareMoonshotTools(opts.Tools, opts.ToolChoice, opts.ToolChoice.Type != "", m.modelID)
	if err != nil {
		return nil, nil, err
	}

	modelFamily := GetModelFamily(m.modelID)
	requestedThinking := modelOpts.Thinking
	requestedReasoningEffort := modelOpts.ReasoningEffort
	preserveReasoning := modelOpts.ReasoningHistory == "preserved"

	if requestedThinking != nil && requestedThinking.HasBudgetTokens {
		message := "Moonshot Chat Completions does not support budget_tokens. Remove budgetTokens; the option has been omitted."
		warnings = append(warnings, types.Warning{
			Type:    "deprecated",
			Setting: "providerOptions.moonshot.thinking.budgetTokens",
			Message: message,
			Details: message,
		})
	}

	var thinkingBody map[string]interface{}
	var reasoningEffort string
	reasoningNone := opts.Reasoning != nil && *opts.Reasoning == types.ReasoningNone

	warnUnsupportedReasoningEffort := func() {
		if requestedReasoningEffort != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningEffort",
				Details: fmt.Sprintf("reasoningEffort is only supported by Kimi K3 and has been omitted for model %q.", m.modelID),
			})
		}
	}

	preservedUnsupportedWarning := func() {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: fmt.Sprintf("reasoningHistory 'preserved' is not supported by model %q", m.modelID),
		})
	}

	switch modelFamily {
	case FamilyKimiK3:
		if requestedThinking != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "thinking",
				Details: "Kimi K3 always reasons and does not accept the thinking field. The option has been omitted.",
			})
		}
		if reasoningNone {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: `reasoning "none"`,
				Details: "Kimi K3 reasoning cannot be disabled.",
			})
		}
		if requestedReasoningEffort != "" {
			reasoningEffort = requestedReasoningEffort
		} else if isCustomReasoning(opts.Reasoning) && !reasoningNone {
			reasoningEffort = mapReasoningToMoonshotEffort(*opts.Reasoning)
		}

	case FamilyKimiK27:
		warnUnsupportedReasoningEffort()
		thinkingDisabled := requestedThinking != nil && requestedThinking.Type == "disabled"
		if thinkingDisabled || reasoningNone {
			feature := `reasoning "none"`
			if thinkingDisabled {
				feature = `thinking.type "disabled"`
			}
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: feature,
				Details: "Kimi K2.7 thinking cannot be disabled.",
			})
		} else if requestedThinking != nil && requestedThinking.Type == "enabled" {
			thinkingBody = map[string]interface{}{"type": "enabled"}
		}

	case FamilyKimiK26:
		warnUnsupportedReasoningEffort()
		thinkingType := moonshotThinkingTypeFromReasoning(requestedThinking, opts.Reasoning)
		if thinkingType != "" || preserveReasoning {
			t := thinkingType
			if t == "" {
				t = "enabled"
			}
			body := map[string]interface{}{"type": t}
			if preserveReasoning {
				body["keep"] = "all"
			}
			thinkingBody = body
		}

	case FamilyKimiK25:
		warnUnsupportedReasoningEffort()
		thinkingType := moonshotThinkingTypeFromReasoning(requestedThinking, opts.Reasoning)
		if thinkingType != "" {
			thinkingBody = map[string]interface{}{"type": thinkingType}
		}
		if preserveReasoning {
			preservedUnsupportedWarning()
		}

	case FamilyMoonshotV1:
		warnUnsupportedReasoningEffort()
		if requestedThinking != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "thinking",
				Details: fmt.Sprintf("thinking is not supported by model %q and has been omitted.", m.modelID),
			})
		}
		if isCustomReasoning(opts.Reasoning) && !reasoningNone {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoning",
				Details: fmt.Sprintf("reasoning is not supported by model %q.", m.modelID),
			})
		}
		if preserveReasoning {
			preservedUnsupportedWarning()
		}

	case FamilyUnknown:
		if reasoningNone {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: `reasoning "none"`,
				Details: "Use providerOptions.moonshot.thinking to control thinking on custom models.",
			})
		}
		if requestedReasoningEffort != "" {
			reasoningEffort = requestedReasoningEffort
		} else if isCustomReasoning(opts.Reasoning) && !reasoningNone {
			reasoningEffort = mapReasoningToMoonshotEffort(*opts.Reasoning)
		}
		if requestedThinking != nil && requestedThinking.Type != "" {
			thinkingBody = map[string]interface{}{"type": requestedThinking.Type}
		}
		if preserveReasoning {
			preservedUnsupportedWarning()
		}
	}

	var responseFormat map[string]interface{}
	var responseFormatType string
	if providerutils.IsJSONResponseFormat(opts.ResponseFormat) {
		schemaVal := providerutils.ResponseFormatJSONSchema(opts.ResponseFormat.Schema)
		if opts.ResponseFormat.Type == "json_object" {
			schemaVal = nil
		}
		schemaMap, _ := schemaVal.(map[string]interface{})
		if SupportsStructuredOutputs(m.modelID) && schemaMap != nil {
			cleaned := make(map[string]interface{}, len(schemaMap))
			for k, v := range schemaMap {
				if k == "$schema" {
					continue
				}
				cleaned[k] = v
			}
			normalized, err := NormalizeJSONSchemaForMFJS(cleaned)
			if err != nil {
				return nil, nil, err
			}
			strict := true
			if modelOpts.HasStrictJSONSchema {
				strict = modelOpts.StrictJSONSchema
			}
			name := opts.ResponseFormat.Name
			if name == "" {
				name = "response"
			}
			responseFormat = map[string]interface{}{
				"type": "json_schema",
				"json_schema": map[string]interface{}{
					"name":   name,
					"strict": strict,
					"schema": normalized,
				},
			}
			responseFormatType = "json_schema"
		} else {
			responseFormat = map[string]interface{}{"type": "json_object"}
			responseFormatType = "json_object"
		}
	}

	var promptMessages []types.Message
	if opts.Prompt.System != "" {
		promptMessages = append(promptMessages, types.Message{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}},
		})
	}
	if opts.Prompt.IsMessages() {
		promptMessages = append(promptMessages, opts.Prompt.Messages...)
	} else if opts.Prompt.IsSimple() {
		promptMessages = append(promptMessages, prompt.SimpleTextToMessages(opts.Prompt.Text)...)
	}

	wireMessages, msgWarnings, err := convertToMoonshotChatMessages(m.modelID, promptMessages, responseFormatType)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, msgWarnings...)
	if wireMessages == nil {
		wireMessages = []interface{}{}
	}

	body := map[string]interface{}{
		"model":    m.modelID,
		"messages": wireMessages,
	}

	if (modelOpts.HasLogprobs && modelOpts.Logprobs) || modelOpts.HasTopLogprobs {
		body["logprobs"] = true
	}
	if modelOpts.HasTopLogprobs {
		body["top_logprobs"] = modelOpts.TopLogprobs
	}
	if opts.MaxTokens != nil {
		body["max_completion_tokens"] = *opts.MaxTokens
	}
	if supportsSampling {
		if opts.Temperature != nil {
			body["temperature"] = *opts.Temperature
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
	}
	if responseFormat != nil {
		body["response_format"] = responseFormat
	}
	if len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if moonshotTools != nil {
		body["tools"] = moonshotTools
	}
	if moonshotToolChoice != nil {
		body["tool_choice"] = moonshotToolChoice
	}
	if modelOpts.HasPrediction {
		body["prediction"] = modelOpts.Prediction
	}
	if thinkingBody != nil {
		body["thinking"] = thinkingBody
	}
	if reasoningEffort != "" {
		body["reasoning_effort"] = reasoningEffort
	}
	if modelOpts.PromptCacheKey != "" {
		body["prompt_cache_key"] = modelOpts.PromptCacheKey
	}
	if modelOpts.SafetyIdentifier != "" {
		body["safety_identifier"] = modelOpts.SafetyIdentifier
	}

	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	return body, append(warnings, toolWarnings...), nil
}

func moonshotFixedSamplingWarning(feature, modelID string) types.Warning {
	return types.Warning{
		Type:    "unsupported",
		Feature: feature,
		Details: fmt.Sprintf("%s is fixed by model %q and has been omitted.", feature, modelID),
	}
}

// isCustomReasoning mirrors TS isCustomReasoning: a caller explicitly chose a
// reasoning level other than the provider-default sentinel.
func isCustomReasoning(r *types.ReasoningLevel) bool {
	return r != nil && *r != types.ReasoningDefault
}

// mapReasoningToMoonshotEffort mirrors the effortMap used for Kimi K3 and
// unknown models: minimal/low -> low, medium/high -> high, xhigh -> max.
func mapReasoningToMoonshotEffort(r types.ReasoningLevel) string {
	switch r {
	case types.ReasoningMinimal, types.ReasoningLow:
		return "low"
	case types.ReasoningMedium, types.ReasoningHigh:
		return "high"
	case types.ReasoningXHigh:
		return "max"
	default:
		return ""
	}
}

// moonshotThinkingTypeFromReasoning resolves the thinking.type used by Kimi
// K2.5/K2.6: an explicit thinking.type option wins; otherwise a custom
// reasoning level maps to "disabled" for reasoning:"none" or "enabled"
// otherwise.
func moonshotThinkingTypeFromReasoning(requestedThinking *moonshotThinkingOption, reasoning *types.ReasoningLevel) string {
	if requestedThinking != nil && requestedThinking.Type != "" {
		return requestedThinking.Type
	}
	if isCustomReasoning(reasoning) {
		if *reasoning == types.ReasoningNone {
			return "disabled"
		}
		return "enabled"
	}
	return ""
}

// convertResponse converts Moonshot API response to SDK format
func (m *LanguageModel) convertResponse(resp moonshotResponse) (*types.GenerateResult, error) {
	if len(resp.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
			Usage:        types.Usage{},
		}, nil
	}

	choice := resp.Choices[0]

	usage, err := ConvertMoonshotUsageRaw(resp.Usage)
	if err != nil {
		return nil, err
	}

	result := &types.GenerateResult{
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        usage,
		RawResponse:  resp,
	}

	if choice.Message.ReasoningContent != "" {
		result.Content = append(result.Content, types.ReasoningContent{Text: choice.Message.ReasoningContent})
	}

	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if len(tc.Function.Arguments) > 0 {
				_ = json.Unmarshal(tc.Function.Arguments, &args)
			}
			if args == nil {
				args = map[string]interface{}{}
			}
			id := tc.ID
			if id == "" {
				id = streaming.GenerateID()
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:           id,
				ToolName:     tc.Function.Name,
				Arguments:    args,
				RawArguments: string(tc.Function.Arguments),
			}
		}
	}

	if choice.Message.Content != "" {
		result.Text = choice.Message.Content
		result.Content = append(result.Content, types.TextContent{Text: choice.Message.Content})
	}

	meta := map[string]json.RawMessage{}
	if len(choice.Logprobs) > 0 && string(choice.Logprobs) != "null" {
		meta["logprobs"] = choice.Logprobs
	}
	if resp.Object != "" {
		if b, err := json.Marshal(resp.Object); err == nil {
			meta["responseObject"] = b
		}
	}
	if choice.Index != nil {
		if b, err := json.Marshal(*choice.Index); err == nil {
			meta["choiceIndex"] = b
		}
	}
	if choice.Message.Role != "" {
		if b, err := json.Marshal(choice.Message.Role); err == nil {
			meta["messageRole"] = b
		}
	}
	if len(choice.Message.ToolCalls) > 0 {
		toolCallTypes := make([]string, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			if tc.Type != "" {
				toolCallTypes = append(toolCallTypes, tc.Type)
			}
		}
		if len(toolCallTypes) > 0 {
			if b, err := json.Marshal(toolCallTypes); err == nil {
				meta["toolCallTypes"] = b
			}
		}
	}
	result.ProviderMetadata = map[string]interface{}{"moonshotai": meta}

	return result, nil
}

// handleError converts errors to SDK error types, preserving the Moonshot
// {error:{message,type,code}} envelope's code when present.
func (m *LanguageModel) handleError(err error) error {
	if parsed := parseMoonshotProviderError(err); parsed != err {
		return parsed
	}
	return providererrors.NewProviderError("moonshot", 0, "", err.Error(), err)
}

// moonshotResponse represents the response from Moonshot chat API
type moonshotResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []moonshotChoice `json:"choices"`
	Usage   json.RawMessage  `json:"usage"`
}

// moonshotChoice represents a choice in the chat response
type moonshotChoice struct {
	Index        *int            `json:"index"`
	Message      moonshotMessage `json:"message"`
	Logprobs     json.RawMessage `json:"logprobs"`
	FinishReason string          `json:"finish_reason"`
}

// moonshotMessage represents a message in the chat
type moonshotMessage struct {
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	ReasoningContent string             `json:"reasoning_content"`
	ToolCalls        []moonshotToolCall `json:"tool_calls,omitempty"`
}

// moonshotToolCall represents a tool call in the response
type moonshotToolCall struct {
	ID       string                   `json:"id"`
	Type     string                   `json:"type"`
	Function moonshotToolCallFunction `json:"function"`
}

// moonshotToolCallFunction represents a tool call function
type moonshotToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ========================================================================
// Streaming Implementation
// ========================================================================

// moonshotStreamChunk is the discriminated union of a successful Chat
// Completions SSE chunk or a Moonshot error envelope, mirroring TS
// moonshotAIChatChunkSchema.
type moonshotStreamChunk struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []moonshotStreamChoice `json:"choices"`
	Usage   json.RawMessage        `json:"usage"`
	Error   *moonshotErrorPayload  `json:"error"`
}

type moonshotStreamChoice struct {
	Index        *int                `json:"index"`
	Delta        moonshotStreamDelta `json:"delta"`
	Logprobs     json.RawMessage     `json:"logprobs"`
	FinishReason *string             `json:"finish_reason"`
	Usage        json.RawMessage     `json:"usage"`
}

type moonshotStreamDelta struct {
	Role             string                        `json:"role"`
	Content          string                        `json:"content"`
	ReasoningContent string                        `json:"reasoning_content"`
	ToolCalls        []moonshotStreamToolCallDelta `json:"tool_calls"`
}

type moonshotStreamToolCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type moonshotLogprobsContent struct {
	Content []json.RawMessage `json:"content"`
}

// moonshotStream implements provider.TextStream for Moonshot streaming
// responses. Tool calls are accumulated across deltas and flushed only at
// the end of the stream, mirroring TS's transform/flush split.
type moonshotStream struct {
	reader           io.ReadCloser
	parser           *streaming.SSEParser
	err              error
	done             bool
	toolCallTracker  *streaming.StreamingToolCallTracker
	flushQueue       []*provider.StreamChunk
	includeRawChunks bool

	isFirstChunk      bool
	isActiveReasoning bool
	isActiveText      bool

	responseObject  string
	choiceIndex     *int
	messageRole     string
	toolCallTypes   map[int]string
	contentLogprobs []json.RawMessage

	topLevelUsage json.RawMessage
	choiceUsage   json.RawMessage
	finishReason  types.FinishReason
}

// newMoonshotStream creates a new Moonshot stream
func newMoonshotStream(reader io.ReadCloser, includeRawChunks bool) *moonshotStream {
	return &moonshotStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		toolCallTracker:  streaming.NewStreamingToolCallTracker(),
		includeRawChunks: includeRawChunks,
		isFirstChunk:     true,
	}
}

// Close implements io.Closer
func (s *moonshotStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *moonshotStream) Next() (*provider.StreamChunk, error) {
	if len(s.flushQueue) > 0 {
		chunk := s.flushQueue[0]
		s.flushQueue = s.flushQueue[1:]
		return chunk, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.done {
		return nil, io.EOF
	}

	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}

	if streaming.IsStreamDone(event) {
		s.done = true
		s.flushQueue = s.buildFlushChunks()
		return s.Next()
	}

	if s.includeRawChunks {
		var raw interface{}
		if err := json.Unmarshal([]byte(event.Data), &raw); err != nil {
			raw = event.Data
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw})
	}

	var chunk moonshotStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		errChunk := &provider.StreamChunk{Type: provider.ChunkTypeError, Text: fmt.Sprintf("failed to parse stream chunk: %v", err)}
		if len(s.flushQueue) > 0 {
			s.flushQueue = append(s.flushQueue, errChunk)
			return s.Next()
		}
		return errChunk, nil
	}

	if chunk.Error != nil {
		s.err = newMoonshotStreamProviderError(*chunk.Error, []byte(event.Data))
		s.finishReason = types.FinishReasonError
		errChunk := &provider.StreamChunk{Type: provider.ChunkTypeError, Text: chunk.Error.Message}
		if len(s.flushQueue) > 0 {
			s.flushQueue = append(s.flushQueue, errChunk)
			return s.Next()
		}
		return errChunk, nil
	}

	if s.isFirstChunk {
		s.isFirstChunk = false
		meta := &provider.ResponseMetadata{ID: chunk.ID, ModelID: chunk.Model}
		if chunk.Created != 0 {
			meta.Timestamp = time.Unix(chunk.Created, 0)
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: meta})
	}

	if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
		s.topLevelUsage = chunk.Usage
	}
	if chunk.Object != "" {
		s.responseObject = chunk.Object
	}

	if len(chunk.Choices) == 0 {
		return s.Next()
	}
	choice := chunk.Choices[0]

	if len(choice.Usage) > 0 && string(choice.Usage) != "null" {
		s.choiceUsage = choice.Usage
	}
	if choice.Index != nil {
		s.choiceIndex = choice.Index
	}
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		s.finishReason = providerutils.MapOpenAIFinishReason(*choice.FinishReason)
	}
	if len(choice.Logprobs) > 0 && string(choice.Logprobs) != "null" {
		var lp moonshotLogprobsContent
		if err := json.Unmarshal(choice.Logprobs, &lp); err == nil {
			s.contentLogprobs = append(s.contentLogprobs, lp.Content...)
		}
	}

	delta := choice.Delta
	if delta.Role != "" {
		s.messageRole = delta.Role
	}

	if delta.ReasoningContent != "" {
		if !s.isActiveReasoning {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"})
			s.isActiveReasoning = true
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "reasoning-0", Reasoning: delta.ReasoningContent})
	}

	if delta.Content != "" {
		if !s.isActiveText {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "txt-0"})
			s.isActiveText = true
		}
		if s.isActiveReasoning {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
			s.isActiveReasoning = false
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: "txt-0", Text: delta.Content})
	}

	if len(delta.ToolCalls) > 0 {
		if s.isActiveReasoning {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
			s.isActiveReasoning = false
		}
		for i, tc := range delta.ToolCalls {
			if tc.Type != "" {
				// toolCallTypes bookkeeping keys on the position within this
				// delta's tool_calls array (the "type" field only ever
				// arrives once, on the delta that starts a given call), kept
				// independent from the correlation index passed to the
				// tracker below so an indexless delta (d53589a: Moonshot may
				// omit "index" entirely) still reaches the tracker's own
				// id/latest-call fallback instead of a synthetic index.
				idx := i
				if tc.Index != nil {
					idx = *tc.Index
				}
				if s.toolCallTypes == nil {
					s.toolCallTypes = map[int]string{}
				}
				s.toolCallTypes[idx] = tc.Type
			}
			// d53589a: pass the provider's index through as-is (nil when
			// omitted) so StreamingToolCallTracker's own id-then-index-then-
			// latest-call lookup order applies -- do not synthesize an index
			// from array position, which would misattribute continuation
			// deltas across separate stream chunks.
			for _, tcc := range s.toolCallTracker.Track(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
				c := tcc
				s.flushQueue = append(s.flushQueue, &c)
			}
		}
	}

	if len(s.flushQueue) > 0 {
		return s.Next()
	}
	return s.Next()
}

// buildFlushChunks closes any open text/reasoning blocks, flushes accumulated
// tool calls, and builds the terminal finish chunk. Mirrors TS doStream's
// TransformStream flush() callback.
func (s *moonshotStream) buildFlushChunks() []*provider.StreamChunk {
	var chunks []*provider.StreamChunk

	if s.isActiveReasoning {
		chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
	}
	if s.isActiveText {
		chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "txt-0"})
	}
	for _, tcc := range s.toolCallTracker.Flush() {
		c := tcc
		chunks = append(chunks, &c)
	}

	finishReason := s.finishReason
	if finishReason == "" {
		finishReason = types.FinishReasonOther
	}

	usageRaw := s.topLevelUsage
	if len(usageRaw) == 0 {
		usageRaw = s.choiceUsage
	}
	usage, usageErr := ConvertMoonshotUsageRaw(usageRaw)
	if usageErr != nil {
		usage = types.Usage{}
	}

	finishChunk := &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     finishReason,
		Usage:            &usage,
		ProviderMetadata: s.buildProviderMetadata(),
	}
	chunks = append(chunks, finishChunk)
	return chunks
}

// buildProviderMetadata assembles the finish chunk's providerMetadata.moonshotai
// object. Always returns a non-nil `{"moonshotai": {...}}` payload, even when
// empty, matching TS's unconditional providerMetadata assignment.
func (s *moonshotStream) buildProviderMetadata() json.RawMessage {
	meta := map[string]json.RawMessage{}
	if len(s.contentLogprobs) > 0 {
		if b, err := json.Marshal(map[string]interface{}{"content": s.contentLogprobs}); err == nil {
			meta["logprobs"] = b
		}
	}
	if s.responseObject != "" {
		if b, err := json.Marshal(s.responseObject); err == nil {
			meta["responseObject"] = b
		}
	}
	if s.choiceIndex != nil {
		if b, err := json.Marshal(*s.choiceIndex); err == nil {
			meta["choiceIndex"] = b
		}
	}
	if s.messageRole != "" {
		if b, err := json.Marshal(s.messageRole); err == nil {
			meta["messageRole"] = b
		}
	}
	if len(s.toolCallTypes) > 0 {
		keys := make([]int, 0, len(s.toolCallTypes))
		for k := range s.toolCallTypes {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		types_ := make([]string, 0, len(keys))
		for _, k := range keys {
			types_ = append(types_, s.toolCallTypes[k])
		}
		if b, err := json.Marshal(types_); err == nil {
			meta["toolCallTypes"] = b
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"moonshotai": meta})
	return raw
}

// Err returns any error that occurred during streaming
func (s *moonshotStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

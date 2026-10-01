package mistral

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

// mistralProviderOptions mirrors mistralLanguageModelChatOptions in
// ai/packages/mistral/src/mistral-chat-language-model-options.ts.
type mistralProviderOptions struct {
	ReasoningEffort    string
	PromptCacheKey     string
	SafePrompt         *bool
	DocumentImageLimit *float64
	DocumentPageLimit  *float64
	ParallelToolCalls  *bool
}

// LanguageModel implements the provider.LanguageModel interface for Mistral AI
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Mistral AI language model
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
	return "mistral"
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

// supportsReasoningEffort returns true for Mistral models that accept reasoning_effort.
func (m *LanguageModel) supportsReasoningEffort() bool {
	return mistralReasoningEffortModelIDs[m.modelID]
}

// checkReasoningWarnings returns warnings for options Mistral does not
// support: topK (mirrors TS's unconditional `if (topK != null)` warning) and
// reasoning configuration on models that do not accept reasoning_effort.
func (m *LanguageModel) checkReasoningWarnings(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault && !m.supportsReasoningEffort() {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoning",
			Details: "This model does not support reasoning configuration.",
		})
	}
	return warnings
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if _, err := extractMistralProviderOptions(opts); err != nil {
		return nil, err
	}
	warnings := m.checkReasoningWarnings(opts)
	reqBody := m.buildRequestBody(opts, false)
	var response mistralResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	responseMetadata := providerutils.BuildResponseMetadata(response.ID, response.Model, response.Created)
	responseMetadata.Headers = result.ResponseHeaders
	result.ResponseMetadata = responseMetadata
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if _, err := extractMistralProviderOptions(opts); err != nil {
		return nil, err
	}
	warnings := m.checkReasoningWarnings(opts)
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
	inner := newMistralStream(httpResp.Body, opts.IncludeRawChunks)
	inner.requestBody = reqBody
	inner.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return streaming.NewWarningsStream(inner, warnings), nil
}

func extractMistralProviderOptions(opts *provider.GenerateOptions) (mistralProviderOptions, error) {
	result := mistralProviderOptions{}
	if opts == nil || opts.ProviderOptions == nil {
		return result, nil
	}
	raw, ok := opts.ProviderOptions["mistral"]
	if !ok || raw == nil {
		return result, nil
	}
	mistralOpts, ok := raw.(map[string]interface{})
	if !ok {
		return result, fmt.Errorf("invalid mistral provider options: expected object")
	}

	if value, ok := mistralOpts["reasoningEffort"]; ok && value != nil {
		effort, ok := value.(string)
		if !ok {
			return result, fmt.Errorf("invalid mistral reasoningEffort: expected string")
		}
		switch effort {
		case "high", "none":
			result.ReasoningEffort = effort
		default:
			return result, fmt.Errorf("invalid mistral reasoningEffort %q: expected \"high\" or \"none\"", effort)
		}
	}

	if value, ok := mistralOpts["promptCacheKey"].(string); ok {
		result.PromptCacheKey = value
	}
	if value, ok := mistralOpts["safePrompt"].(bool); ok {
		result.SafePrompt = &value
	}
	if value, ok := mistralOpts["documentImageLimit"].(float64); ok {
		result.DocumentImageLimit = &value
	}
	if value, ok := mistralOpts["documentPageLimit"].(float64); ok {
		result.DocumentPageLimit = &value
	}
	if value, ok := mistralOpts["parallelToolCalls"].(bool); ok {
		result.ParallelToolCalls = &value
	}

	return result, nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	provOpts, _ := extractMistralProviderOptions(opts)

	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
	}
	if opts.Prompt.IsMessages() {
		body["messages"] = ConvertToMistralChatMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = ConvertToMistralChatMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": opts.Prompt.System,
		}
		body["messages"] = append([]map[string]interface{}{systemMsg}, messages...)
	}
	if provOpts.SafePrompt != nil {
		body["safe_prompt"] = *provOpts.SafePrompt
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
		body["random_seed"] = *opts.Seed
	}
	if provOpts.DocumentImageLimit != nil {
		body["document_image_limit"] = *provOpts.DocumentImageLimit
	}
	if provOpts.DocumentPageLimit != nil {
		body["document_page_limit"] = *provOpts.DocumentPageLimit
	}
	if provOpts.PromptCacheKey != "" {
		body["prompt_cache_key"] = provOpts.PromptCacheKey
	}
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
		if provOpts.ParallelToolCalls != nil {
			body["parallel_tool_calls"] = *provOpts.ParallelToolCalls
		}
	}
	// Response format (TS mistral-chat-language-model.ts): structuredOutputs
	// defaults to true, strictJsonSchema defaults to false. JSON mode without a
	// schema also injects a JSON instruction into the system message.
	mistralOptions, _ := opts.ProviderOptions["mistral"].(map[string]interface{})
	if format, _ := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs: providerutils.BoolOption(mistralOptions, "structuredOutputs", true),
		StrictJSONSchema:  providerutils.BoolOption(mistralOptions, "strictJsonSchema", false),
	}); format != nil {
		body["response_format"] = format
		if providerutils.ResponseFormatJSONSchema(opts.ResponseFormat.Schema) == nil || opts.ResponseFormat.Type == "json_object" {
			injectMistralJSONInstruction(body)
		}
	}
	// Map top-level Reasoning to Mistral reasoning_effort.
	// Only selected Mistral models support reasoning_effort.
	// Other models emit a CallWarning in DoGenerate instead.
	// Mistral maps none → "none"; all non-default levels → "high".
	// provider-default → omit.
	if m.supportsReasoningEffort() {
		if opts.Reasoning != nil {
			switch *opts.Reasoning {
			case types.ReasoningNone:
				body["reasoning_effort"] = "none"
			case types.ReasoningMinimal, types.ReasoningLow, types.ReasoningMedium, types.ReasoningHigh, types.ReasoningXHigh:
				body["reasoning_effort"] = "high"
				// ReasoningDefault: omit
			}
		}
		if provOpts.ReasoningEffort != "" {
			body["reasoning_effort"] = provOpts.ReasoningEffort
		}
	}
	return body
}

func (m *LanguageModel) convertResponse(response mistralResponse) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
		}
	}
	choice := response.Choices[0]
	text, reasoningParts := parseMistralMessageContent(choice.Message.Content)
	result := &types.GenerateResult{
		Text:            text,
		FinishReason:    mapMistralFinishReason(choice.FinishReason),
		RawFinishReason: choice.FinishReason,
		Usage:           convertMistralUsage(response.Usage),
		RawResponse:     response,
	}
	for _, reasoningText := range reasoningParts {
		result.Content = append(result.Content, types.ReasoningContent{Text: reasoningText})
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
	return result
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("mistral", 0, "", err.Error(), err)
}

// convertMistralUsage converts Mistral usage to detailed Usage struct
// Implements v6.0 detailed token tracking with optional detailed fields.
//
// raw is the full JSON-decoded usage object exactly as Mistral sent it. It is
// decoded twice here: once into the typed mistralUsage struct used to derive
// the InputDetails/OutputDetails breakdown below, and once into a generic
// map[string]interface{} stored verbatim on Usage.Raw (matching TS
// convertMistralUsage, which sets `raw: usage` to the whole decoded object,
// not a hand-picked subset of fields).
func convertMistralUsage(raw json.RawMessage) types.Usage {
	var usage mistralUsage
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &usage)
	}

	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)

	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}

	// cacheRead precedence exactly matches TS convertMistralUsage:
	// num_cached_tokens ?? prompt_tokens_details.cached_tokens ??
	// prompt_token_details.cached_tokens ?? 0. Mistral has no cache-write
	// concept and never reports reasoning/text/image token breakdowns, so
	// those are intentionally not derived here (TS always leaves cacheWrite
	// and outputTokens.reasoning undefined).
	var cachedTokens int64
	switch {
	case usage.NumCachedTokens != nil:
		cachedTokens = int64(*usage.NumCachedTokens)
	case usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil:
		cachedTokens = int64(*usage.PromptTokensDetails.CachedTokens)
	case usage.PromptTokenDetails != nil && usage.PromptTokenDetails.CachedTokens != nil:
		cachedTokens = int64(*usage.PromptTokenDetails.CachedTokens)
	}

	noCacheTokens := promptTokens - cachedTokens
	inputDetails := &types.InputTokenDetails{NoCacheTokens: &noCacheTokens}
	// TS: cacheRead: cacheReadTokens || undefined (0 is falsy -> omitted).
	if cachedTokens != 0 {
		inputDetails.CacheReadTokens = &cachedTokens
	}
	result.InputDetails = inputDetails

	// TS: outputTokens.text is always the full completion token count;
	// outputTokens.reasoning is always undefined.
	result.OutputDetails = &types.OutputTokenDetails{TextTokens: &completionTokens}

	// Store the full raw usage object (every field Mistral returned), not a
	// hand-picked subset, matching TS convertMistralUsage's `raw: usage`.
	var rawMap map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &rawMap)
	}
	if rawMap == nil {
		rawMap = map[string]interface{}{}
	}
	result.Raw = rawMap

	return result
}

// mapMistralFinishReason extends the standard OpenAI finish reason mapping with
// "model_length", a Mistral-specific variant of "length".
func mapMistralFinishReason(reason string) types.FinishReason {
	if reason == "model_length" {
		return types.FinishReasonLength
	}
	return providerutils.MapOpenAIFinishReason(reason)
}

type mistralResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role string `json:"role"`
			// Content is either a plain string or an array of content parts
			// (type "text"/"thinking") for thinking-enabled models; see
			// parseMistralMessageContent.
			Content   json.RawMessage `json:"content"`
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
	Usage json.RawMessage `json:"usage"`
}

// mistralContentPart is one element of a Mistral message content array, used
// by thinking-enabled models. Only the fields needed to extract visible text
// and reasoning are modeled here.
type mistralContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"thinking"`
}

// parseMistralMessageContent parses a Mistral chat message's "content" field,
// which is either a plain string or an array of content parts (text/thinking).
// It returns the concatenated visible text and the text of each "thinking"
// part in the order they appear, mirroring TS's extractTextContent /
// extractReasoningContent in mistral-chat-language-model.ts.
func parseMistralMessageContent(raw json.RawMessage) (text string, reasoningParts []string) {
	if len(raw) == 0 {
		return "", nil
	}
	switch raw[0] {
	case '"':
		_ = json.Unmarshal(raw, &text)
		return text, nil
	case '[':
		var parts []mistralContentPart
		if err := json.Unmarshal(raw, &parts); err != nil {
			return "", nil
		}
		var textBuilder strings.Builder
		for _, part := range parts {
			switch part.Type {
			case "text":
				textBuilder.WriteString(part.Text)
			case "thinking":
				var thinkingText strings.Builder
				for _, t := range part.Thinking {
					thinkingText.WriteString(t.Text)
				}
				if thinkingText.Len() > 0 {
					reasoningParts = append(reasoningParts, thinkingText.String())
				}
			}
		}
		return textBuilder.String(), reasoningParts
	default:
		return "", nil
	}
}

// mistralUsage represents Mistral usage information
// mistralUsage mirrors the declared fields of TS mistralUsageSchema
// (convert-mistral-usage.ts) exactly. Mistral returns more fields than
// declared here (service_tier, request_count, prompt_audio_seconds, ...);
// convertMistralUsage separately decodes the same bytes into a generic map
// for Usage.Raw so nothing is dropped.
type mistralUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	NumCachedTokens  *int `json:"num_cached_tokens,omitempty"`

	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`

	// Legacy Mistral spelling kept for API compatibility.
	PromptTokenDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
	} `json:"prompt_token_details,omitempty"`
}

// mistralStream implements provider.TextStream for Mistral AI SSE responses.
// It handles delta.content as either a plain string or an array of content
// parts (type "text" or "thinking") for thinking-enabled models.
type mistralStream struct {
	reader                  io.ReadCloser
	parser                  *streaming.SSEParser
	err                     error
	toolCallTracker         *streaming.StreamingToolCallTracker
	flushQueue              []*provider.StreamChunk
	isActiveReasoning       bool
	includeRawChunks        bool
	responseHeaders         map[string]string
	responseMetadataEmitted bool
	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *mistralStream) RequestBody() interface{} { return s.requestBody }

func newMistralStream(reader io.ReadCloser, includeRawChunks ...bool) *mistralStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return &mistralStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		toolCallTracker:  streaming.NewStreamingToolCallTracker(),
		includeRawChunks: emitRaw,
	}
}

func (s *mistralStream) Close() error { return s.reader.Close() }
func (s *mistralStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *mistralStream) Next() (*provider.StreamChunk, error) {
nextLoop:
	for {
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

		// Parse using a struct where content is json.RawMessage to handle
		// both plain-string and content-array formats.
		var chunkData struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content   json.RawMessage `json:"content"`
					ToolCalls []struct {
						// Index is nullish in TS's mistralChatChunkSchema
						// (index: z.number().nullish()); a *int (rather than a
						// bare int defaulting to 0) preserves "omitted" as
						// distinct from index 0 for the tracker's lookup order.
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
			errorChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: fmt.Sprintf("mistral: failed to parse stream chunk: %v", err),
			}
			if rawQueued {
				s.flushQueue = append(s.flushQueue, errorChunk)
				continue
			}
			return errorChunk, nil
		}
		if !s.responseMetadataEmitted && (chunkData.ID != "" || chunkData.Model != "" || chunkData.Created != 0 || len(s.responseHeaders) > 0) {
			s.responseMetadataEmitted = true
			meta := &provider.ResponseMetadata{
				ID:      chunkData.ID,
				ModelID: chunkData.Model,
				Headers: s.responseHeaders,
			}
			if chunkData.Created != 0 {
				meta.Timestamp = time.Unix(chunkData.Created, 0)
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeResponseMetadata,
				ResponseMetadata: meta,
			})
		}

		if len(chunkData.Choices) == 0 {
			continue
		}
		choice := chunkData.Choices[0]

		// Parse delta content: try plain string first, then array of parts.
		contentRaw := choice.Delta.Content
		if len(contentRaw) > 0 && contentRaw[0] == '[' {
			// Array of content parts — used in thinking mode.
			var parts []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"thinking"`
			}
			if err := json.Unmarshal(contentRaw, &parts); err == nil {
				for _, part := range parts {
					switch part.Type {
					case "thinking":
						// Collect thinking text
						var thinkingText string
						for _, t := range part.Thinking {
							thinkingText += t.Text
						}
						if thinkingText != "" {
							if !s.isActiveReasoning {
								s.isActiveReasoning = true
								s.flushQueue = append([]*provider.StreamChunk{
									{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
									{Type: provider.ChunkTypeReasoning, Reasoning: thinkingText, ID: "reasoning-0"},
								}, s.flushQueue...)
								// Must re-enter Next()'s outer loop now (matching the
								// old `return s.Next()`), not just continue the
								// `range parts` loop: base behavior abandons any
								// remaining parts in this same content array once
								// the first reasoning-start fires.
								continue nextLoop
							}
							s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
								Type:      provider.ChunkTypeReasoning,
								Reasoning: thinkingText,
								ID:        "reasoning-0",
							})
						}
					case "text":
						if part.Text != "" {
							if s.isActiveReasoning {
								s.isActiveReasoning = false
								s.flushQueue = append(s.flushQueue,
									&provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
									&provider.StreamChunk{Type: provider.ChunkTypeText, Text: part.Text},
								)
							} else {
								s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
									Type: provider.ChunkTypeText,
									Text: part.Text,
								})
							}
						}
					}
				}
				if len(s.flushQueue) > 0 {
					if choice.FinishReason != "" {
						s.flushMistralToolCalls(choice.FinishReason)
					}
					continue
				}
			}
		} else if len(contentRaw) > 0 && contentRaw[0] == '"' {
			// Plain string content.
			var text string
			if err := json.Unmarshal(contentRaw, &text); err == nil && text != "" {
				if s.isActiveReasoning {
					s.isActiveReasoning = false
					s.flushQueue = append([]*provider.StreamChunk{
						{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
						{Type: provider.ChunkTypeText, Text: text},
					}, s.flushQueue...)
					if choice.FinishReason != "" {
						s.flushMistralToolCalls(choice.FinishReason)
					}
					continue
				}
				if choice.FinishReason != "" {
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeText, Text: text})
					s.flushMistralToolCalls(choice.FinishReason)
					continue
				}
				textChunk := &provider.StreamChunk{Type: provider.ChunkTypeText, Text: text}
				if len(s.flushQueue) > 0 {
					s.flushQueue = append(s.flushQueue, textChunk)
					continue
				}
				return textChunk, nil
			}
		}

		// Tool call deltas — tracked via the shared StreamingToolCallTracker
		// (TS mistral-chat-language-model.ts uses the same tracker), which emits
		// tool-input-start/delta chunks as arguments arrive and only finalizes
		// into a tool-call chunk on Flush (never mid-stream, matching Mistral's
		// finish-time-only semantics).
		if len(choice.Delta.ToolCalls) > 0 {
			for _, tc := range choice.Delta.ToolCalls {
				for _, chunk := range s.toolCallTracker.Track(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
					c := chunk
					s.flushQueue = append(s.flushQueue, &c)
				}
			}
		}

		// Finish event — flush tool calls and emit finish chunk.
		if choice.FinishReason != "" {
			s.flushMistralToolCalls(choice.FinishReason)
			continue
		}

		continue

	}
}

func (s *mistralStream) flushMistralToolCalls(finishReason string) {
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
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
		Type:            provider.ChunkTypeFinish,
		FinishReason:    mapMistralFinishReason(finishReason),
		RawFinishReason: finishReason,
	})
}

// injectMistralJSONInstruction mirrors injectJsonInstructionIntoMessages
// (provider-utils) for JSON mode without a schema: the generic JSON
// instruction is merged into the leading system message (or a new one).
//
// TS always merges into a single system message because its
// LanguageModelV4Message system role only ever carries string content. Go's
// unified Message allows a system message with multiple content parts (e.g.
// several TextContent parts), which ToOpenAIMessages then serializes as a
// []map[string]interface{} content array rather than a string. That case
// must still be merged into the existing leading system message -- by
// appending a text part -- rather than falling through to prepending a
// second system message.
func injectMistralJSONInstruction(body map[string]interface{}) {
	const instruction = "You MUST answer with JSON."
	messages, _ := body["messages"].([]map[string]interface{})
	if len(messages) > 0 && messages[0]["role"] == "system" {
		switch content := messages[0]["content"].(type) {
		case string:
			if content != "" {
				messages[0]["content"] = content + "\n\n" + instruction
			} else {
				messages[0]["content"] = instruction
			}
			return
		case []map[string]interface{}:
			messages[0]["content"] = append(content, map[string]interface{}{"type": "text", "text": instruction})
			return
		case nil:
			messages[0]["content"] = instruction
			return
		}
	}
	body["messages"] = append([]map[string]interface{}{{"role": "system", "content": instruction}}, messages...)
}

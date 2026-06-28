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
	reqBody := m.buildRequestBody(opts, false)

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
	reqBody := m.buildRequestBody(opts, true)

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
	return streaming.NewWarningsStream(inner, nil), nil
}

// buildRequestBody builds the OpenAI API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
	}

	// Extract store flag early — needed before message conversion so we can
	// filter unencrypted reasoning parts from assistant messages when store=false.
	// storeExplicit tracks whether the caller set the flag (so we only send it
	// in the request body when explicitly provided, not by default).
	store := true // effective value; default is server-side persistence
	storeExplicit := false
	if opts.ProviderOptions != nil {
		if openaiOpts, ok := opts.ProviderOptions["openai"].(map[string]interface{}); ok {
			if v, ok := openaiOpts["store"].(bool); ok {
				store = v
				storeExplicit = true
			}
		}
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

	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(convertMessages(opts.Prompt.Messages))
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}

	// Add system message if present.
	// Reasoning models (o1, o3, o4-mini, gpt-5.x non-chat) require the
	// "developer" role instead of "system" per OpenAI's API specification.
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		role := "system"
		if isReasoningModel(m.modelID) {
			role = "developer"
		}
		systemMsg := map[string]interface{}{
			"role":    role,
			"content": opts.Prompt.System,
		}
		body["messages"] = append([]map[string]interface{}{systemMsg}, messages...)
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

	// Add tools if present
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}

	// Add response format if present
	if opts.ResponseFormat != nil {
		body["response_format"] = map[string]interface{}{
			"type": opts.ResponseFormat.Type,
		}
	}

	// Map top-level Reasoning to OpenAI reasoning_effort.
	// none → "disabled", minimal/low → "low", medium → "medium", high/xhigh → "high".
	// provider-default → omit (let OpenAI use its own default).
	if opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningNone:
			body["reasoning_effort"] = "disabled"
		case types.ReasoningMinimal, types.ReasoningLow:
			body["reasoning_effort"] = "low"
		case types.ReasoningMedium:
			body["reasoning_effort"] = "medium"
		case types.ReasoningHigh, types.ReasoningXHigh:
			body["reasoning_effort"] = "high"
			// ReasoningDefault: omit
		}
	}

	// Apply OpenAI-specific provider options
	if opts.ProviderOptions != nil {
		if openaiOpts, ok := opts.ProviderOptions["openai"].(map[string]interface{}); ok {
			// Add prompt cache retention if present.
			// Supports "in_memory" (default) and "24h" (for gpt-5.1 series).
			if promptCacheRetention, ok := openaiOpts["promptCacheRetention"].(string); ok {
				body["prompt_cache_retention"] = promptCacheRetention
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
		}
	}

	return body
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

		// Extract text
		if choice.Message.Content != "" {
			result.Text = choice.Message.Content
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
	if cachedTokens > 0 || textTokens != nil || imageTokens != nil {
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedTokens,
			// OpenAI doesn't report cache write tokens separately
			CacheWriteTokens: nil,
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

// openAIMessage represents an OpenAI message
type openAIMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
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
// Uses an allowlist approach — only known reasoning model prefixes are matched.
// This avoids inadvertently changing the role for fine-tuned or custom models.
//
// Matches: o1*, o3*, o4-mini*, gpt-5* (except gpt-5-chat*)
// Non-matches: gpt-4*, gpt-3.5*, gpt-5-chat-latest, etc.
func isReasoningModel(modelID string) bool {
	return strings.HasPrefix(modelID, "o1") ||
		strings.HasPrefix(modelID, "o3") ||
		strings.HasPrefix(modelID, "o4-mini") ||
		(strings.HasPrefix(modelID, "gpt-5") && !strings.HasPrefix(modelID, "gpt-5-chat"))
}

// supportsNonReasoningParameters reports whether a reasoning model accepts
// standard sampling parameters when reasoning effort is disabled.
func supportsNonReasoningParameters(modelID string) bool {
	return strings.HasPrefix(modelID, "gpt-5.1") ||
		strings.HasPrefix(modelID, "gpt-5.2") ||
		strings.HasPrefix(modelID, "gpt-5.3") ||
		strings.HasPrefix(modelID, "gpt-5.4") ||
		strings.HasPrefix(modelID, "gpt-5.5")
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

// Read implements io.Reader
func (s *openAIStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *openAIStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *openAIStream) Next() (*provider.StreamChunk, error) {
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
			return s.Next()
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
		}
		if len(s.flushQueue) > 0 {
			s.flushQueue = append(s.flushQueue, errorChunk)
			return s.Next()
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
				return s.Next()
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
			return s.Next()
		}

		// Finish chunk — flush all accumulated tool calls first.
		if choice.FinishReason != nil {
			if rawChunk != nil && !s.outputStarted {
				s.pendingRaw = append(s.pendingRaw, rawChunk)
			}
			s.flushPendingMetadata()
			s.flushOpenAIToolCalls(*choice.FinishReason)
			s.outputStarted = true
			return s.Next()
		}
	}

	if rawChunk != nil {
		s.pendingRaw = append(s.pendingRaw, rawChunk)
	}
	// Empty chunk, get next
	return s.Next()
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
		Type:         provider.ChunkTypeFinish,
		FinishReason: providerutils.MapOpenAIFinishReason(finishReason),
	})
}

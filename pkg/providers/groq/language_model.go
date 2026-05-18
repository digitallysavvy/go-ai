package groq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// LanguageModel implements the provider.LanguageModel interface for Groq
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Groq language model
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
	return "groq"
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
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts, false)
	var response groqResponse
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
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts, true)
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
	inner := newGroqStream(httpResp.Body, opts.IncludeRawChunks)
	inner.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return streaming.NewWarningsStream(inner, warnings), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body, _ := m.buildRequestBodyWithWarnings(opts, stream)
	return body
}

func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning) {
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
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
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
	// Map top-level Reasoning to Groq reasoning_effort.
	// none and provider-default → omit (Groq does not accept "disabled" for none).
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
	compatibleOptions, warnings := providerutils.ResolveOpenAICompatibleProviderOptions("groq", opts.ProviderOptions)
	warnings = append(warnings, providerutils.OpenAICompatibleCommonOptionWarnings(compatibleOptions)...)
	providerutils.ApplyOpenAICompatibleCommonRequestOptions(body, compatibleOptions)
	return body, warnings
}

func (m *LanguageModel) convertResponse(response groqResponse) *types.GenerateResult {
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
		Usage:        convertGroqUsage(response.Usage),
		RawResponse:  response,
	}
	if choice.Message.Reasoning != "" {
		result.Content = append(result.Content, types.ReasoningContent{Text: choice.Message.Reasoning})
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

func convertGroqUsage(usage groqUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)
	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}

	// Parse detailed token information if available
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

	// Set input details (always set, even if just with basic values)
	if cachedTokens > 0 || textTokens != nil || imageTokens != nil {
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedTokens,
			CacheWriteTokens: nil,
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
		}
	} else {
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &promptTokens,
			CacheReadTokens:  nil,
			CacheWriteTokens: nil,
		}
	}

	// Set output details
	if reasoningTokens > 0 {
		textOutputTokens := completionTokens - reasoningTokens
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textOutputTokens,
			ReasoningTokens: &reasoningTokens,
		}
	} else {
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &completionTokens,
			ReasoningTokens: nil,
		}
	}

	// Store raw usage
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

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("groq", 0, "", err.Error(), err)
}

type groqResponse struct {
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
			Reasoning string `json:"reasoning"` // reasoning mode
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
	Usage groqUsage `json:"usage"`
}

type groqUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Detailed token breakdown (OpenAI-compatible)
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

type groqStreamChunk struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Error   json.RawMessage `json:"error,omitempty"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"` // Groq uses "reasoning" field
			ToolCalls []struct {
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
	// XGroq carries Groq-specific metadata including streaming usage.
	// Groq does not use stream_options; usage arrives in this envelope instead.
	XGroq *struct {
		Usage *groqUsage `json:"usage,omitempty"`
	} `json:"x_groq,omitempty"`
}

type groqStream struct {
	reader            io.ReadCloser
	parser            *streaming.SSEParser
	err               error
	toolCallTracker   *streaming.StreamingToolCallTracker
	flushQueue        []*provider.StreamChunk
	isActiveReasoning bool
	pendingUsage      *groqUsage // captured from x_groq.usage
	includeRawChunks  bool
	responseHeaders   map[string]string
	metadataEmitted   bool
}

func newGroqStream(reader io.ReadCloser, includeRawChunks ...bool) *groqStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return &groqStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		toolCallTracker:  streaming.NewStreamingToolCallTracker(),
		includeRawChunks: emitRaw,
	}
}

func (s *groqStream) Close() error { return s.reader.Close() }

func (s *groqStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, error) {
	if len(s.flushQueue) == 0 {
		return chunk, nil
	}
	s.flushQueue = append(s.flushQueue, chunk)
	return s.Next()
}

func (s *groqStream) Next() (*provider.StreamChunk, error) {
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
	var chunkData groqStreamChunk
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
			Text: groqStreamErrorText(chunkData.Error),
		})
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
	// Capture usage from x_groq envelope (Groq's streaming usage mechanism).
	if chunkData.XGroq != nil && chunkData.XGroq.Usage != nil {
		s.pendingUsage = chunkData.XGroq.Usage
	}
	if len(chunkData.Choices) > 0 {
		choice := chunkData.Choices[0]

		// Handle reasoning content.
		if choice.Delta.Reasoning != "" {
			if !s.isActiveReasoning {
				s.isActiveReasoning = true
				s.flushQueue = append([]*provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
					{Type: provider.ChunkTypeReasoning, Reasoning: choice.Delta.Reasoning, ID: "reasoning-0"},
				}, s.flushQueue...)
				return s.Next()
			}
			return s.emitParsedChunk(&provider.StreamChunk{
				Type:      provider.ChunkTypeReasoning,
				Reasoning: choice.Delta.Reasoning,
				ID:        "reasoning-0",
			})
		}

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
				s.flushGroqToolCalls(choice.FinishReason)
				return s.Next()
			}
			return s.Next()
		}
		if choice.FinishReason != "" {
			s.flushGroqToolCalls(choice.FinishReason)
			return s.Next()
		}
	}
	return s.Next()
}

func groqStreamErrorText(raw json.RawMessage) string {
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

func (s *groqStream) flushGroqToolCalls(finishReason string) {
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
	finishChunk := &provider.StreamChunk{
		Type:         provider.ChunkTypeFinish,
		FinishReason: providerutils.MapOpenAIFinishReason(finishReason),
	}
	if s.pendingUsage != nil {
		u := convertGroqUsage(*s.pendingUsage)
		finishChunk.Usage = &u
	}
	s.flushQueue = append(s.flushQueue, finishChunk)
}

func (s *groqStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "openai"
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

	// Make API request
	var response openAIResponse
	err := m.provider.client.PostJSON(ctx, "/chat/completions", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response to GenerateResult
	return m.convertResponse(response), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming enabled
	reqBody := m.buildRequestBody(opts, true)

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
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

	// Create stream wrapper
	return newOpenAIStream(httpResp.Body), nil
}

// buildRequestBody builds the OpenAI API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}

	// Convert messages
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}

	// Add system message if present
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		systemMsg := map[string]interface{}{
			"role":    "system",
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
				json.Unmarshal([]byte(tc.Function.Arguments), &args)

				result.ToolCalls[i] = types.ToolCall{
					ID:        tc.ID,
					ToolName:  tc.Function.Name,
					Arguments: args,
				}
			}
		}

		// Extract finish reason
		result.FinishReason = types.FinishReason(choice.FinishReason)
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

	// Calculate reasoning tokens
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}

	// Set input token details
	if cachedTokens > 0 {
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedTokens,
			// OpenAI doesn't report cache write tokens separately
			CacheWriteTokens: nil,
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
	return providererrors.NewProviderError("openai", 0, "", err.Error(), err)
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
	} `json:"prompt_tokens_details,omitempty"`

	CompletionTokensDetails *struct {
		ReasoningTokens             *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens    *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens    *int `json:"rejected_prediction_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

// openAIMessage represents an OpenAI message
type openAIMessage struct {
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []openAIToolCall  `json:"tool_calls,omitempty"`
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

// openAIStream implements provider.TextStream for OpenAI streaming
type openAIStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

// newOpenAIStream creates a new OpenAI stream
func newOpenAIStream(reader io.ReadCloser) *openAIStream {
	return &openAIStream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
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

	// Parse the event data as JSON
	var chunkData struct {
		Choices []struct {
			Delta struct {
				Content   string            `json:"content"`
				ToolCalls []openAIToolCall  `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		return nil, fmt.Errorf("failed to parse stream chunk: %w", err)
	}

	// Extract chunk data
	if len(chunkData.Choices) > 0 {
		choice := chunkData.Choices[0]

		// Text chunk
		if choice.Delta.Content != "" {
			return &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: choice.Delta.Content,
			}, nil
		}

		// Tool call chunk
		if len(choice.Delta.ToolCalls) > 0 {
			// TODO: Handle streaming tool calls
		}

		// Finish chunk
		if choice.FinishReason != nil {
			return &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: types.FinishReason(*choice.FinishReason),
			}, nil
		}
	}

	// Empty chunk, get next
	return s.Next()
}

// Err returns any error that occurred during streaming
func (s *openAIStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

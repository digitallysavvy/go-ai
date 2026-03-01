package moonshot

import (
	"context"
	"encoding/json"
	"fmt"
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
	return true // Moonshot models support JSON mode
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return false // Moonshot models don't support image inputs currently
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody := m.buildRequestBody(opts, false)

	var response moonshotResponse
	err := m.prov.client.PostJSON(ctx, "/chat/completions", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	return m.convertResponse(response), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming enabled
	reqBody := m.buildRequestBody(opts, true)

	// Make streaming API request
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

	// Create stream wrapper
	return newMoonshotStream(httpResp.Body), nil
}

// buildRequestBody builds the API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}

	// Convert prompt to messages using providerutils
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

	// Add standard parameters
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

	// Response format (structured output)
	if opts.ResponseFormat != nil {
		if opts.ResponseFormat.Type == "json" {
			if opts.ResponseFormat.Schema != nil {
				body["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"schema":      opts.ResponseFormat.Schema,
						"name":        opts.ResponseFormat.Name,
						"description": opts.ResponseFormat.Description,
					},
				}
			} else {
				body["response_format"] = map[string]interface{}{
					"type": "json_object",
				}
			}
		}
	}

	// Tools
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}

	// Moonshot-specific options from provider metadata
	if opts.ProviderOptions != nil {
		if moonshotOpts, ok := opts.ProviderOptions["moonshot"].(map[string]interface{}); ok {
			// Thinking/reasoning support (for K2.5 and thinking models)
			if thinking, ok := moonshotOpts["thinking"].(map[string]interface{}); ok {
				thinkingBody := make(map[string]interface{})

				if thinkingType, ok := thinking["type"].(string); ok {
					thinkingBody["type"] = thinkingType
				}

				if budgetTokens, ok := thinking["budget_tokens"].(int); ok {
					thinkingBody["budget_tokens"] = budgetTokens
				}

				if len(thinkingBody) > 0 {
					body["thinking"] = thinkingBody
				}
			}

			// Reasoning history (for preserving thinking chains)
			if reasoningHistory, ok := moonshotOpts["reasoning_history"].(string); ok {
				body["reasoning_history"] = reasoningHistory
			}
		}
	}

	// Streaming options
	if stream {
		body["stream_options"] = map[string]interface{}{
			"include_usage": true,
		}
	}

	return body
}

// convertResponse converts Moonshot API response to SDK format
func (m *LanguageModel) convertResponse(resp moonshotResponse) *types.GenerateResult {
	if len(resp.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
			Usage:        types.Usage{},
		}
	}

	choice := resp.Choices[0]
	result := &types.GenerateResult{
		Text:         choice.Message.Content,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        ConvertMoonshotUsage(resp.Usage),
		RawResponse:  resp,
	}

	// Add tool calls if present
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if len(tc.Function.Arguments) > 0 {
				if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
					// If unmarshal fails, use empty args
					args = make(map[string]interface{})
				}
			} else {
				args = make(map[string]interface{})
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

// handleError converts errors to SDK error types
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("moonshot", 0, "", err.Error(), err)
}


// moonshotResponse represents the response from Moonshot chat API
type moonshotResponse struct {
	ID                string           `json:"id"`
	Object            string           `json:"object"`
	Created           int64            `json:"created"`
	Model             string           `json:"model"`
	SystemFingerprint string           `json:"system_fingerprint,omitempty"`
	Choices           []moonshotChoice `json:"choices"`
	Usage             MoonshotUsage    `json:"usage"`
}

// moonshotChoice represents a choice in the chat response
type moonshotChoice struct {
	Index        int             `json:"index"`
	Message      moonshotMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// moonshotMessage represents a message in the chat
type moonshotMessage struct {
	Role      string              `json:"role"`
	Content   string              `json:"content"`
	ToolCalls []moonshotToolCall  `json:"tool_calls,omitempty"`
}

// moonshotToolCall represents a tool call in the response
type moonshotToolCall struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Function moonshotToolCallFunction  `json:"function"`
}

// moonshotToolCallFunction represents a tool call function
type moonshotToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ========================================================================
// Streaming Implementation
// ========================================================================

// moonshotStream implements provider.TextStream for Moonshot streaming responses
type moonshotStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

// newMoonshotStream creates a new Moonshot stream
func newMoonshotStream(reader io.ReadCloser) *moonshotStream {
	return &moonshotStream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
	}
}

// Read implements io.Reader
func (s *moonshotStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *moonshotStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *moonshotStream) Next() (*provider.StreamChunk, error) {
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
				Content   string              `json:"content"`
				ToolCalls []moonshotToolCall  `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *MoonshotUsage `json:"usage,omitempty"`
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
			// Handle streaming tool calls
			toolCall := choice.Delta.ToolCalls[0]
			var args map[string]interface{}
			if len(toolCall.Function.Arguments) > 0 {
				if err := json.Unmarshal(toolCall.Function.Arguments, &args); err != nil {
					args = make(map[string]interface{})
				}
			} else {
				args = make(map[string]interface{})
			}

			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:        toolCall.ID,
					ToolName:  toolCall.Function.Name,
					Arguments: args,
				},
			}, nil
		}

		// Finish chunk with usage
		if choice.FinishReason != nil {
			chunk := &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: providerutils.MapOpenAIFinishReason(*choice.FinishReason),
			}

			// Add usage if present
			if chunkData.Usage != nil {
				usage := ConvertMoonshotUsage(*chunkData.Usage)
				chunk.Usage = &usage
			}

			return chunk, nil
		}
	}

	// Empty chunk, get next
	return s.Next()
}

// Err returns any error that occurred during streaming
func (s *moonshotStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

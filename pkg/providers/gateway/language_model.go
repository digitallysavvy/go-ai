package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for AI Gateway
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new AI Gateway language model
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
	return "gateway"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Gateway passes through to underlying models, assume tools are supported
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	// Gateway passes through to underlying models, assume structured output is supported
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Gateway passes through to underlying models, assume image input is supported
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Convert options to request body
	reqBody, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Create headers for the request
	headers := m.getModelConfigHeaders(false)

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders()
	AddO11yHeaders(headers, o11y)

	// Make API request
	var result types.GenerateResult
	err = m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/language-model",
		Body:    reqBody,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleError(err)
	}

	return &result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Convert options to request body
	reqBody, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Create headers for the request
	headers := m.getModelConfigHeaders(true)
	headers["Accept"] = "text/event-stream"

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders()
	AddO11yHeaders(headers, o11y)

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/language-model",
		Body:    reqBody,
		Headers: headers,
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Create streaming parser and wrap it in a proper TextStream implementation
	stream := streaming.NewSSEParser(httpResp.Body)

	return &gatewayTextStream{
		parser: stream,
		body:   httpResp.Body,
	}, nil
}

// gatewayTextStream implements provider.TextStream for Gateway provider
type gatewayTextStream struct {
	parser *streaming.SSEParser
	body   io.ReadCloser
	err    error
}

// gatewayStreamChunk represents a chunk from the Gateway streaming API
// This follows the LanguageModelV3StreamPart format
type gatewayStreamChunk struct {
	Type string `json:"type"`

	// For text-delta chunks
	TextDelta string `json:"textDelta,omitempty"`

	// For reasoning-delta chunks
	ReasoningDelta string `json:"reasoningDelta,omitempty"`

	// For tool-call chunks
	ToolCallID       string                 `json:"toolCallId,omitempty"`
	ToolCallType     string                 `json:"toolCallType,omitempty"`
	ToolCallName     string                 `json:"toolCallName,omitempty"`
	ToolCallArgs     string                 `json:"toolCallArgs,omitempty"`
	ToolCallArgsText string                 `json:"toolCallArgsText,omitempty"`
	ToolCallArgsJSON map[string]interface{} `json:"-"` // Parsed from ToolCallArgs

	// For finish chunks
	FinishReason types.FinishReason `json:"finishReason,omitempty"`

	// For usage chunks
	Usage *struct {
		PromptTokens     *int64 `json:"promptTokens,omitempty"`
		CompletionTokens *int64 `json:"completionTokens,omitempty"`
		TotalTokens      *int64 `json:"totalTokens,omitempty"`
	} `json:"usage,omitempty"`

	// For error chunks
	Error string `json:"error,omitempty"`
}

// Read implements io.Reader
func (s *gatewayTextStream) Read(p []byte) (n int, err error) {
	return s.body.Read(p)
}

// Close implements io.Closer
func (s *gatewayTextStream) Close() error {
	return s.body.Close()
}

// Next returns the next chunk in the stream
func (s *gatewayTextStream) Next() (*provider.StreamChunk, error) {
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

	// Parse the JSON data from the SSE event
	var chunk gatewayStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		s.err = fmt.Errorf("failed to parse stream chunk: %w", err)
		return nil, s.err
	}

	// Convert Gateway chunk to provider StreamChunk
	return s.convertChunk(&chunk)
}

// convertChunk converts a Gateway stream chunk to a provider StreamChunk
func (s *gatewayTextStream) convertChunk(chunk *gatewayStreamChunk) (*provider.StreamChunk, error) {
	switch chunk.Type {
	case "text-delta":
		return &provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: chunk.TextDelta,
		}, nil

	case "reasoning-delta":
		return &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: chunk.ReasoningDelta,
		}, nil

	case "tool-call", "tool-call-delta":
		// Parse tool call arguments if present
		var args map[string]interface{}
		if chunk.ToolCallArgs != "" {
			json.Unmarshal([]byte(chunk.ToolCallArgs), &args)
		} else if chunk.ToolCallArgsText != "" {
			json.Unmarshal([]byte(chunk.ToolCallArgsText), &args)
		}

		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:        chunk.ToolCallID,
				ToolName:  chunk.ToolCallName,
				Arguments: args,
			},
		}, nil

	case "finish":
		result := &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			FinishReason: chunk.FinishReason,
		}

		// Add usage information if present
		if chunk.Usage != nil {
			result.Usage = &types.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}

		return result, nil

	case "usage":
		if chunk.Usage != nil {
			return &provider.StreamChunk{
				Type: provider.ChunkTypeUsage,
				Usage: &types.Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
					TotalTokens:  chunk.Usage.TotalTokens,
				},
			}, nil
		}
		// Skip empty usage chunks
		return s.Next()

	case "error":
		return &provider.StreamChunk{
			Type:        provider.ChunkTypeError,
			AbortReason: chunk.Error,
		}, fmt.Errorf("stream error: %s", chunk.Error)

	default:
		// Skip unknown chunk types and get next chunk
		return s.Next()
	}
}

// Err implements TextStream
func (s *gatewayTextStream) Err() error {
	return s.err
}

// buildRequestBody converts GenerateOptions to the request body format
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, streaming bool) (map[string]interface{}, error) {
	body := make(map[string]interface{})

	// Convert prompt - gateway expects the V3 format
	if opts.Prompt.IsMessages() {
		// Convert messages to the format expected by gateway
		messages := make([]map[string]interface{}, 0, len(opts.Prompt.Messages))
		for _, msg := range opts.Prompt.Messages {
			message := map[string]interface{}{
				"role": string(msg.Role),
			}

			// Convert content parts
			if len(msg.Content) > 0 {
				content := make([]map[string]interface{}, 0, len(msg.Content))
				for _, part := range msg.Content {
					contentPart, err := m.convertContentPart(part)
					if err != nil {
						return nil, err
					}
					content = append(content, contentPart)
				}
				message["content"] = content
			}

			messages = append(messages, message)
		}
		body["messages"] = messages
	} else if opts.Prompt.IsSimple() {
		// Simple text prompt - convert to user message
		body["messages"] = []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": opts.Prompt.Text,
					},
				},
			},
		}
	}

	// Add system message if present
	if opts.Prompt.System != "" {
		body["system"] = opts.Prompt.System
	}

	// Add temperature
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}

	// Add top-p
	if opts.TopP != nil {
		body["topP"] = *opts.TopP
	}

	// Add top-k
	if opts.TopK != nil {
		body["topK"] = *opts.TopK
	}

	// Add presence penalty
	if opts.PresencePenalty != nil {
		body["presencePenalty"] = *opts.PresencePenalty
	}

	// Add frequency penalty
	if opts.FrequencyPenalty != nil {
		body["frequencyPenalty"] = *opts.FrequencyPenalty
	}

	// Add stop sequences
	if len(opts.StopSequences) > 0 {
		body["stopSequences"] = opts.StopSequences
	}

	// Add max tokens
	if opts.MaxTokens != nil {
		body["maxTokens"] = *opts.MaxTokens
	}

	// Add seed
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// Add response format
	if opts.ResponseFormat != nil {
		format := map[string]interface{}{
			"type": opts.ResponseFormat.Type,
		}
		if opts.ResponseFormat.Schema != nil {
			format["schema"] = opts.ResponseFormat.Schema
		}
		if opts.ResponseFormat.Name != "" {
			format["name"] = opts.ResponseFormat.Name
		}
		if opts.ResponseFormat.Description != "" {
			format["description"] = opts.ResponseFormat.Description
		}
		body["responseFormat"] = format
	}

	// Add tools
	if len(opts.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(opts.Tools))
		for _, tool := range opts.Tools {
			toolMap := map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			}
			if tool.ProviderExecuted {
				toolMap["providerExecuted"] = true
			}
			tools = append(tools, toolMap)
		}
		body["tools"] = tools
	}

	// Add tool choice
	if opts.ToolChoice.Type != "" {
		body["toolChoice"] = map[string]interface{}{
			"type":     string(opts.ToolChoice.Type),
			"toolName": opts.ToolChoice.ToolName,
		}
	}

	// Add headers
	if len(opts.Headers) > 0 {
		body["headers"] = opts.Headers
	}

	return body, nil
}

// convertContentPart converts a content part to the gateway format
func (m *LanguageModel) convertContentPart(part types.ContentPart) (map[string]interface{}, error) {
	switch v := part.(type) {
	case types.TextContent:
		return map[string]interface{}{
			"type": "text",
			"text": v.Text,
		}, nil

	case types.ImageContent:
		result := map[string]interface{}{
			"type": "image",
		}

		// Handle image URL or data
		if v.URL != "" {
			result["image"] = v.URL
		} else if len(v.Image) > 0 {
			// Image is binary data - encode as base64 data URL
			mediaType := v.MimeType
			if mediaType == "" {
				mediaType = "image/jpeg"
			}
			encoded := base64.StdEncoding.EncodeToString(v.Image)
			result["image"] = fmt.Sprintf("data:%s;base64,%s", mediaType, encoded)
		}

		return result, nil

	case types.FileContent:
		result := map[string]interface{}{
			"type": "file",
		}

		// Handle file data - encode as base64 data URL
		if len(v.Data) > 0 {
			mediaType := v.MimeType
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			encoded := base64.StdEncoding.EncodeToString(v.Data)
			result["data"] = fmt.Sprintf("data:%s;base64,%s", mediaType, encoded)
		}

		if v.MimeType != "" {
			result["mimeType"] = v.MimeType
		}

		return result, nil

	default:
		return nil, fmt.Errorf("unsupported content part type: %T", part)
	}
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *LanguageModel) getModelConfigHeaders(streaming bool) map[string]string {
	return map[string]string{
		"ai-language-model-specification-version": "3",
		"ai-language-model-id":                    m.modelID,
		"ai-language-model-streaming":             fmt.Sprintf("%t", streaming),
	}
}

// handleError converts errors to appropriate provider errors
func (m *LanguageModel) handleError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a provider error
	if providererrors.IsProviderError(err) {
		return err
	}

	// Return as ProviderError
	return providererrors.NewProviderError("gateway", 0, "", err.Error(), err)
}

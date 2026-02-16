package openresponses

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
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for Open Responses
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Open Responses language model
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
	return m.provider.config.Name
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Tool support depends on the underlying model
	// Return true as many local models support tools
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Image support depends on the underlying model
	// Return true for vision models (users need to verify their model supports it)
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody, warnings := m.buildRequestBody(opts, false)

	// Make API request
	var response OpenResponsesResponse
	err := m.provider.client.PostJSON(ctx, "", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Check for error in response
	if response.Error != nil {
		return nil, fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}

	// Convert response to GenerateResult
	result := m.convertResponse(response)
	result.Warnings = warnings

	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming enabled
	reqBody, warnings := m.buildRequestBody(opts, true)

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Create stream wrapper
	return newOpenResponsesStream(httpResp.Body, warnings), nil
}

// buildRequestBody builds the Open Responses API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning) {
	var warnings []types.Warning

	// Convert messages to Open Responses format
	input, instructions, conversionWarnings := ConvertToOpenResponsesInput(opts.Prompt.Messages, opts.Prompt.System)
	warnings = append(warnings, conversionWarnings...)

	body := map[string]interface{}{
		"model":  m.modelID,
		"input":  input,
		"stream": stream,
	}

	// Add instructions if present
	if instructions != "" {
		body["instructions"] = instructions
	}

	// Add optional parameters
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
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

	// Note: Open Responses doesn't support stopSequences, topK, or seed
	// These are ignored with warnings
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "stopSequences not supported by Open Responses API",
		})
	}
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "topK not supported by Open Responses API",
		})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "seed not supported by Open Responses API",
		})
	}

	// Add tools if present
	if len(opts.Tools) > 0 {
		openResponsesTools := convertToolsToOpenResponses(opts.Tools)
		body["tools"] = openResponsesTools

		// Convert tool choice
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = convertToolChoiceToOpenResponses(opts.ToolChoice)
		}
	}

	// Add response format if present
	if opts.ResponseFormat != nil {
		textConfig := map[string]interface{}{
			"format": map[string]interface{}{
				"type": "json_schema",
			},
		}
		if opts.ResponseFormat.Schema != nil {
			textConfig["format"].(map[string]interface{})["name"] = "response"
			textConfig["format"].(map[string]interface{})["schema"] = opts.ResponseFormat.Schema
			textConfig["format"].(map[string]interface{})["strict"] = true
		}
		body["text"] = textConfig
	}

	return body, warnings
}

// convertToolsToOpenResponses converts AI SDK tools to Open Responses format
func convertToolsToOpenResponses(tools []types.Tool) []FunctionTool {
	result := make([]FunctionTool, 0, len(tools))

	for _, t := range tools {
		ft := FunctionTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
		}

		if t.Parameters != nil {
			// Convert parameters to map if possible
			if paramsMap, ok := t.Parameters.(map[string]interface{}); ok {
				ft.Parameters = paramsMap
			}
		}

		ft.Strict = t.Strict

		result = append(result, ft)
	}

	return result
}

// convertToolChoiceToOpenResponses converts tool choice to Open Responses format
func convertToolChoiceToOpenResponses(toolChoice types.ToolChoice) interface{} {
	switch toolChoice.Type {
	case "auto":
		return "auto"
	case "required":
		return "required"
	case "none":
		return "none"
	case "tool":
		return map[string]interface{}{
			"type": "function",
			"name": toolChoice.ToolName,
		}
	default:
		return "auto"
	}
}

// convertResponse converts an Open Responses response to GenerateResult
func (m *LanguageModel) convertResponse(response OpenResponsesResponse) *types.GenerateResult {
	result := &types.GenerateResult{
		RawResponse: response,
	}

	// Convert usage information
	if response.Usage != nil {
		result.Usage = convertOpenResponsesUsage(response.Usage)
	}

	// Extract content from output items
	var textParts []string
	var toolCalls []types.ToolCall
	hasToolCalls := false

	for _, item := range response.Output {
		switch item.Type {
		case "message":
			// Extract text from message content
			for _, part := range item.Content {
				if part.Type == "output_text" {
					textParts = append(textParts, part.Text)
				}
			}

		case "reasoning":
			// Extract reasoning text (could be surfaced separately in future)
			for _, part := range item.Content {
				if part.Type == "reasoning_text" {
					// For now, include reasoning as regular text
					textParts = append(textParts, part.Text)
				}
			}

		case "function_call":
			// Extract tool call
			hasToolCalls = true

			var args map[string]interface{}
			if item.Arguments != "" {
				json.Unmarshal([]byte(item.Arguments), &args)
			}

			toolCalls = append(toolCalls, types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: args,
			})
		}
	}

	// Combine text parts
	if len(textParts) > 0 {
		result.Text = textParts[0]
		if len(textParts) > 1 {
			// Join multiple text parts
			fullText := ""
			for _, part := range textParts {
				fullText += part
			}
			result.Text = fullText
		}
	}

	// Set tool calls
	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
	}

	// Determine finish reason
	finishReason := ""
	if response.IncompleteDetails != nil {
		finishReason = response.IncompleteDetails.Reason
	}
	result.FinishReason = MapOpenResponsesFinishReason(finishReason, hasToolCalls)

	return result
}

// convertOpenResponsesUsage converts Open Responses usage to AI SDK usage
func convertOpenResponsesUsage(usage *Usage) types.Usage {
	inputTokens := int64(usage.InputTokens)
	outputTokens := int64(usage.OutputTokens)
	totalTokens := int64(usage.TotalTokens)

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Calculate cached tokens
	var cachedTokens int64
	if usage.InputTokensDetails != nil {
		cachedTokens = int64(usage.InputTokensDetails.CachedTokens)
	}

	// Calculate reasoning tokens
	var reasoningTokens int64
	if usage.OutputTokensDetails != nil {
		reasoningTokens = int64(usage.OutputTokensDetails.ReasoningTokens)
	}

	// Set input token details
	if cachedTokens > 0 {
		noCacheTokens := inputTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedTokens,
		}
	}

	// Set output token details
	if reasoningTokens > 0 {
		textTokens := outputTokens - reasoningTokens
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		}
	}

	// Store raw usage
	result.Raw = map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
		"total_tokens":  usage.TotalTokens,
	}

	return result
}

// handleError converts errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError(m.provider.config.Name, 0, "", err.Error(), err)
}

// openResponsesStream implements provider.TextStream for Open Responses streaming
type openResponsesStream struct {
	reader   io.ReadCloser
	parser   *streaming.SSEParser
	err      error
	warnings []types.Warning

	// Track state for tool calls and finish reason
	toolCallsByItemID     map[string]*toolCallState
	hasToolCalls          bool
	finishReason          types.FinishReason
	usage                 *types.Usage
}

// toolCallState tracks the state of a tool call during streaming
type toolCallState struct {
	ID        string
	ToolName  string
	ArgsJSON  string // Accumulated JSON string
}

// newOpenResponsesStream creates a new Open Responses stream
func newOpenResponsesStream(reader io.ReadCloser, warnings []types.Warning) *openResponsesStream {
	return &openResponsesStream{
		reader:            reader,
		parser:            streaming.NewSSEParser(reader),
		warnings:          warnings,
		toolCallsByItemID: make(map[string]*toolCallState),
		finishReason:      types.FinishReasonOther,
	}
}

// Read implements io.Reader
func (s *openResponsesStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *openResponsesStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *openResponsesStream) Next() (*provider.StreamChunk, error) {
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
	var streamEvent StreamEvent
	if err := json.Unmarshal([]byte(event.Data), &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to parse stream event: %w", err)
	}

	// Handle different event types
	return s.handleStreamEvent(&streamEvent)
}

// handleStreamEvent processes a stream event and returns appropriate chunk
func (s *openResponsesStream) handleStreamEvent(event *StreamEvent) (*provider.StreamChunk, error) {
	switch event.Type {
	case "response.output_item.added":
		// New output item started
		if event.Item != nil && event.Item.Type == "function_call" {
			// Initialize tool call tracking
			s.toolCallsByItemID[event.Item.ID] = &toolCallState{
				ID:       event.Item.CallID,
				ToolName: event.Item.Name,
				ArgsJSON: "",
			}
		}
		// Don't emit a chunk for this event
		return s.Next()

	case "response.output_text.delta":
		// Text delta
		return &provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: event.Delta,
		}, nil

	case "response.function_call_arguments.delta":
		// Tool call arguments delta (accumulate but don't emit yet)
		if toolCallState, ok := s.toolCallsByItemID[event.ItemID]; ok {
			toolCallState.ArgsJSON += event.Delta
		}
		return s.Next()

	case "response.function_call_arguments.done":
		// Tool call arguments complete
		if toolCallState, ok := s.toolCallsByItemID[event.ItemID]; ok {
			// Use the final arguments from the event if provided
			if event.Arguments != "" {
				toolCallState.ArgsJSON = event.Arguments
			}
		}
		return s.Next()

	case "response.output_item.done":
		// Output item complete
		if event.Item != nil && event.Item.Type == "function_call" {
			s.hasToolCalls = true

			// Get the tool call state and convert to ToolCall
			if toolCallState, ok := s.toolCallsByItemID[event.Item.ID]; ok {
				// Parse arguments as JSON
				var args map[string]interface{}
				if toolCallState.ArgsJSON != "" {
					json.Unmarshal([]byte(toolCallState.ArgsJSON), &args)
				}

				// Create final tool call
				toolCall := &types.ToolCall{
					ID:        toolCallState.ID,
					ToolName:  toolCallState.ToolName,
					Arguments: args,
				}

				// Emit tool call chunk
				return &provider.StreamChunk{
					Type:     provider.ChunkTypeToolCall,
					ToolCall: toolCall,
				}, nil
			}
		}
		return s.Next()

	case "response.completed":
		// Response completed successfully
		finishReason := ""
		if event.Response != nil && event.Response.IncompleteDetails != nil {
			finishReason = event.Response.IncompleteDetails.Reason
		}
		s.finishReason = MapOpenResponsesFinishReason(finishReason, s.hasToolCalls)

		// Update usage
		if event.Response != nil && event.Response.Usage != nil {
			usage := convertOpenResponsesUsage(event.Response.Usage)
			s.usage = &usage
		}

		return s.Next()

	case "response.incomplete":
		// Response incomplete
		finishReason := ""
		if event.Response != nil && event.Response.IncompleteDetails != nil {
			finishReason = event.Response.IncompleteDetails.Reason
		}
		s.finishReason = MapOpenResponsesFinishReason(finishReason, s.hasToolCalls)

		// Update usage
		if event.Response != nil && event.Response.Usage != nil {
			usage := convertOpenResponsesUsage(event.Response.Usage)
			s.usage = &usage
		}

		return s.Next()

	case "response.failed":
		// Response failed
		s.finishReason = types.FinishReasonError
		if event.Response != nil && event.Error != nil {
			s.err = fmt.Errorf("%s: %s", event.Error.Code, event.Error.Message)
			return nil, s.err
		}
		return s.Next()

	case "error":
		// Error event
		if event.Error != nil {
			s.err = fmt.Errorf("%s: %s", event.Error.Code, event.Error.Message)
			return nil, s.err
		}
		return s.Next()

	default:
		// Unknown event type, skip
		return s.Next()
	}
}

// Err returns any error that occurred during streaming
func (s *openResponsesStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

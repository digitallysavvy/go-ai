package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	bedrock "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// BedrockAnthropicLanguageModel implements the provider.LanguageModel interface for Bedrock Anthropic
type BedrockAnthropicLanguageModel struct {
	provider *BedrockAnthropicProvider
	modelID  string
}

// SpecificationVersion returns the specification version
func (m *BedrockAnthropicLanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *BedrockAnthropicLanguageModel) Provider() string {
	return "bedrock-anthropic"
}

// ModelID returns the model ID
func (m *BedrockAnthropicLanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *BedrockAnthropicLanguageModel) SupportsTools() bool {
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *BedrockAnthropicLanguageModel) SupportsStructuredOutput() bool {
	// Bedrock doesn't support anthropic_beta header needed for structured output
	return false
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *BedrockAnthropicLanguageModel) SupportsImageInput() bool {
	// All Claude 3+ models on Bedrock support vision
	return true
}

// DoGenerate performs non-streaming text generation
func (m *BedrockAnthropicLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts, false)

	// Build URL
	endpoint := fmt.Sprintf("%s/model/%s/invoke", m.provider.baseURL, url.PathEscape(m.modelID))

	// Create HTTP request
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Authenticate request
	if err := m.authenticateRequest(req, bodyBytes); err != nil {
		return nil, fmt.Errorf("failed to authenticate request: %w", err)
	}

	// Make request
	resp, err := m.provider.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, m.handleErrorResponse(resp)
	}

	// Parse response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBytes, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to GenerateResult
	return m.convertResponse(anthropicResp, reqBody), nil
}

// DoStream performs streaming text generation
func (m *BedrockAnthropicLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming
	reqBody := m.buildRequestBody(opts, true)

	// Build streaming URL
	endpoint := fmt.Sprintf("%s/model/%s/invoke-with-response-stream", m.provider.baseURL, url.PathEscape(m.modelID))

	// Create HTTP request
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.amazon.eventstream")

	// Authenticate request
	if err := m.authenticateRequest(req, bodyBytes); err != nil {
		return nil, fmt.Errorf("failed to authenticate request: %w", err)
	}

	// Make request
	resp, err := m.provider.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, m.handleErrorResponse(resp)
	}

	// Transform Bedrock event stream to SSE format
	sseReader := NewSSEStreamReader(resp.Body)

	// Create SSE stream
	return newBedrockAnthropicStream(sseReader, resp.Body), nil
}

// buildRequestBody builds the request body for Bedrock Anthropic API
func (m *BedrockAnthropicLanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"anthropic_version": AnthropicVersion,
	}

	// Convert messages (Anthropic format)
	if opts.Prompt.IsMessages() {
		messages := prompt.ToAnthropicMessages(opts.Prompt.Messages)
		// Insert cache points in messages if configured
		if m.provider.cacheConfig != nil && len(m.provider.cacheConfig.CacheMessageIndices) > 0 {
			messages = m.insertMessageCachePoints(messages, m.provider.cacheConfig)
		}
		body["messages"] = messages
	} else if opts.Prompt.IsSimple() {
		messages := prompt.ToAnthropicMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
		// Insert cache points in messages if configured
		if m.provider.cacheConfig != nil && len(m.provider.cacheConfig.CacheMessageIndices) > 0 {
			messages = m.insertMessageCachePoints(messages, m.provider.cacheConfig)
		}
		body["messages"] = messages
	}

	// Add system message separately (Anthropic requires this)
	if opts.Prompt.System != "" {
		// If cache config is enabled for system, use array format with cache point
		if m.provider.cacheConfig != nil && m.provider.cacheConfig.CacheSystem {
			systemBlocks := []interface{}{
				map[string]interface{}{"text": opts.Prompt.System},
			}

			// Add cache point after system message
			cachePoint := CreateBedrockCachePoint(m.provider.cacheConfig.TTL)
			systemBlocks = append(systemBlocks, map[string]interface{}{
				"cachePoint": map[string]interface{}{
					"type": cachePoint.Type,
					"ttl":  cachePoint.TTL,
				},
			})

			body["system"] = systemBlocks
		} else {
			body["system"] = opts.Prompt.System
		}
	}

	// Set max_tokens (required by Anthropic)
	maxTokens := 4096 // Default
	if opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}
	body["max_tokens"] = maxTokens

	// Add optional parameters
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.TopK != nil {
		body["top_k"] = *opts.TopK
	}
	if len(opts.StopSequences) > 0 {
		body["stop_sequences"] = opts.StopSequences
	}

	// Add output_config when ResponseFormat is specified.
	// Anthropic API requires output_config.format instead of the old output_format field.
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type != "" {
		outputConfig := map[string]interface{}{}
		if opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_schema" {
			if opts.ResponseFormat.Schema != nil {
				outputConfig["format"] = map[string]interface{}{
					"type":   "json_schema",
					"schema": opts.ResponseFormat.Schema,
				}
			}
		}
		if len(outputConfig) > 0 {
			body["output_config"] = outputConfig
		}
	}

	// Add tools if present - with Bedrock-specific transformations
	if len(opts.Tools) > 0 {
		// Prepare tools (upgrade versions and map names)
		preparedTools := m.provider.PrepareTools(opts.Tools)

		// Convert to Anthropic format
		toolsArray := tool.ToAnthropicFormat(preparedTools)

		// If cache config is enabled for tools, append cache point
		if m.provider.cacheConfig != nil && m.provider.cacheConfig.CacheTools {
			cachePoint := CreateBedrockCachePoint(m.provider.cacheConfig.TTL)
			// Convert to []interface{} for appending
			toolsWithCache := make([]interface{}, len(toolsArray)+1)
			for i, t := range toolsArray {
				toolsWithCache[i] = t
			}
			toolsWithCache[len(toolsArray)] = map[string]interface{}{
				"cachePoint": map[string]interface{}{
					"type": cachePoint.Type,
					"ttl":  cachePoint.TTL,
				},
			}
			body["tools"] = toolsWithCache
		} else {
			body["tools"] = toolsArray
		}

		// Handle tool choice
		if opts.ToolChoice.Type != "" {
			toolChoiceData := tool.ConvertToolChoiceToAnthropic(opts.ToolChoice)
			// Remove disable_parallel_tool_use if present (not supported by Bedrock)
			if tcMap, ok := toolChoiceData.(map[string]interface{}); ok {
				delete(tcMap, "disable_parallel_tool_use")
			}
			body["tool_choice"] = toolChoiceData
		}

		// Add anthropic_beta header if computer use tools are present
		betaHeaders := m.provider.GetBetaHeaders(preparedTools)
		if len(betaHeaders) > 0 {
			body["anthropic_beta"] = betaHeaders
		}
	}

	return body
}

// authenticateRequest adds authentication to the request
func (m *BedrockAnthropicLanguageModel) authenticateRequest(req *http.Request, payload []byte) error {
	// Prefer bearer token if available
	if m.provider.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.provider.bearerToken)
		return nil
	}

	// Fall back to SigV4
	if m.provider.credentials == nil {
		return fmt.Errorf("no credentials provided: set either BearerToken or Credentials")
	}

	signer := bedrock.NewAWSSigner(
		m.provider.credentials.AccessKeyID,
		m.provider.credentials.SecretAccessKey,
		m.provider.credentials.SessionToken,
		m.provider.region,
	)

	return signer.SignRequest(req, payload)
}

// handleErrorResponse handles error responses from the API
func (m *BedrockAnthropicLanguageModel) handleErrorResponse(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(resp.Body)

	var errorResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &errorResp); err != nil {
		return &providererrors.ProviderError{
			Provider:   "bedrock-anthropic",
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes)),
		}
	}

	return &providererrors.ProviderError{
		Provider:   "bedrock-anthropic",
		StatusCode: resp.StatusCode,
		ErrorCode:  errorResp.Error.Type,
		Message:    errorResp.Error.Message,
	}
}

// convertResponse converts Anthropic response to GenerateResult
func (m *BedrockAnthropicLanguageModel) convertResponse(response anthropicResponse, requestBody map[string]interface{}) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:       convertAnthropicUsage(response.Usage),
		RawResponse: response,
		RawRequest:  requestBody,
	}

	// Extract text from content blocks
	var textParts []string
	for _, content := range response.Content {
		if content.Type == "text" {
			textParts = append(textParts, content.Text)
		}
	}
	if len(textParts) > 0 {
		result.Text = textParts[0]
	}

	// Extract tool calls
	for _, content := range response.Content {
		if content.Type == "tool_use" {
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:        content.ID,
				ToolName:  content.Name,
				Arguments: content.Input,
			})
		}
	}

	// Map finish reason
	switch response.StopReason {
	case "end_turn":
		result.FinishReason = types.FinishReasonStop
	case "max_tokens":
		result.FinishReason = types.FinishReasonLength
	case "tool_use":
		result.FinishReason = types.FinishReasonToolCalls
	case "stop_sequence":
		result.FinishReason = types.FinishReasonStop
	default:
		result.FinishReason = types.FinishReasonOther
	}

	return result
}

// anthropicResponse represents the response from Anthropic API
type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

// anthropicContent represents a content block in the response
type anthropicContent struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

// anthropicUsage represents token usage information
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// convertAnthropicUsage converts Anthropic usage to types.Usage
func convertAnthropicUsage(usage anthropicUsage) types.Usage {
	inputTokens := int64(usage.InputTokens)
	outputTokens := int64(usage.OutputTokens)
	totalTokens := int64(usage.InputTokens + usage.OutputTokens)

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Add input token details if cache tokens are present
	if usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0 {
		cacheWriteTokens := int64(usage.CacheCreationInputTokens)
		cacheReadTokens := int64(usage.CacheReadInputTokens)
		result.InputDetails = &types.InputTokenDetails{
			CacheWriteTokens: &cacheWriteTokens,
			CacheReadTokens:  &cacheReadTokens,
		}
	}

	return result
}

// insertMessageCachePoints inserts cache points at specified message indices
func (m *BedrockAnthropicLanguageModel) insertMessageCachePoints(messages []map[string]interface{}, config *CacheConfig) []map[string]interface{} {
	if len(config.CacheMessageIndices) == 0 {
		return messages
	}

	cachePoint := CreateBedrockCachePoint(config.TTL)
	cachePointBlock := map[string]interface{}{
		"type": "cachePoint",
		"cachePoint": map[string]interface{}{
			"type": cachePoint.Type,
			"ttl":  cachePoint.TTL,
		},
	}

	// Process messages and insert cache points
	for _, idx := range config.CacheMessageIndices {
		if idx < 0 || idx >= len(messages) {
			continue // Skip invalid indices
		}

		// Get the message at the index
		messageMap := messages[idx]

		// Get the content array
		content, ok := messageMap["content"].([]interface{})
		if !ok {
			// If content is a string, convert to array format
			if contentStr, isStr := messageMap["content"].(string); isStr {
				content = []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": contentStr,
					},
				}
			} else {
				continue
			}
		}

		// Append cache point to content
		content = append(content, cachePointBlock)
		messageMap["content"] = content
	}

	return messages
}

// bedrockAnthropicStream implements provider.TextStream for Bedrock Anthropic streaming
type bedrockAnthropicStream struct {
	sseReader io.ReadCloser
	rawBody   io.Closer
	parser    *streaming.SSEParser
	err       error
}

// newBedrockAnthropicStream creates a new streaming response wrapper
func newBedrockAnthropicStream(sseReader io.ReadCloser, rawBody io.Closer) *bedrockAnthropicStream {
	return &bedrockAnthropicStream{
		sseReader: sseReader,
		rawBody:   rawBody,
		parser:    streaming.NewSSEParser(sseReader),
	}
}

// Next returns the next chunk from the stream
func (s *bedrockAnthropicStream) Next() (*provider.StreamChunk, error) {
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}

	// Handle [DONE] event
	if event.Data == "[DONE]" {
		return nil, io.EOF
	}

	// Parse event data
	var eventData map[string]interface{}
	if err := json.Unmarshal([]byte(event.Data), &eventData); err != nil {
		return nil, fmt.Errorf("failed to parse event data: %w", err)
	}

	// Handle different event types
	eventType, _ := eventData["type"].(string)

	switch eventType {
	case "content_block_delta":
		// Text content delta
		if delta, ok := eventData["delta"].(map[string]interface{}); ok {
			if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
				if text, ok := delta["text"].(string); ok {
					return &provider.StreamChunk{
						Type: provider.ChunkTypeText,
						Text: text,
					}, nil
				}
			}
		}

	case "message_delta":
		// Usage information or finish
		if delta, ok := eventData["delta"].(map[string]interface{}); ok {
			if stopReason, ok := delta["stop_reason"].(string); ok && stopReason != "" {
				finishReason := types.FinishReasonOther
				switch stopReason {
				case "end_turn":
					finishReason = types.FinishReasonStop
				case "max_tokens":
					finishReason = types.FinishReasonLength
				case "tool_use":
					finishReason = types.FinishReasonToolCalls
				case "stop_sequence":
					finishReason = types.FinishReasonStop
				}

				return &provider.StreamChunk{
					Type:         provider.ChunkTypeFinish,
					FinishReason: finishReason,
				}, nil
			}
		}

		// Usage delta
		if usage, ok := eventData["usage"].(map[string]interface{}); ok {
			outputTokens := int64(0)
			if ot, ok := usage["output_tokens"].(float64); ok {
				outputTokens = int64(ot)
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeUsage,
				Usage: &types.Usage{
					OutputTokens: &outputTokens,
				},
			}, nil
		}

	case "message_stop":
		// End of stream
		return nil, io.EOF

	case "error":
		// Error event
		if errData, ok := eventData["error"].(map[string]interface{}); ok {
			errMsg, _ := errData["message"].(string)
			return nil, fmt.Errorf("stream error: %s", errMsg)
		}
		return nil, fmt.Errorf("stream error: %v", eventData)
	}

	// Skip unknown events
	return s.Next()
}

// Read implements io.Reader interface
func (s *bedrockAnthropicStream) Read(p []byte) (n int, err error) {
	return s.sseReader.Read(p)
}

// Close closes the stream
func (s *bedrockAnthropicStream) Close() error {
	if s.sseReader != nil {
		s.sseReader.Close()
	}
	if s.rawBody != nil {
		return s.rawBody.Close()
	}
	return nil
}

// Err returns any error that occurred during streaming
func (s *bedrockAnthropicStream) Err() error {
	return s.err
}

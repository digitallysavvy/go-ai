package anthropic

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

// LanguageModel implements the provider.LanguageModel interface for Anthropic
type LanguageModel struct {
	provider *Provider
	modelID  string
	options  *ModelOptions
}

// NewLanguageModel creates a new Anthropic language model
func NewLanguageModel(provider *Provider, modelID string, options *ModelOptions) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
		options:  options,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "anthropic"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Claude 3+ models support tools
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	// Claude doesn't have native JSON mode like OpenAI
	// but can be instructed to output JSON
	return false
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Claude 3+ models support vision
	return m.modelID == "claude-3-opus-20240229" ||
		   m.modelID == "claude-3-sonnet-20240229" ||
		   m.modelID == "claude-3-haiku-20240307" ||
		   m.modelID == "claude-3-5-sonnet-20241022"
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts, false)

	// Add beta headers if context management is enabled
	betaHeaders := m.getBetaHeaders()
	if len(betaHeaders) > 0 {
		// Need to make request with custom headers
		httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
			Method: http.MethodPost,
			Path:   "/v1/messages",
			Body:   reqBody,
			Headers: map[string]string{
				"anthropic-beta": betaHeaders,
			},
		})
		if err != nil {
			return nil, m.handleError(err)
		}
		defer httpResp.Body.Close()

		// Parse response
		var response anthropicResponse
		if err := json.NewDecoder(httpResp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// Convert response to GenerateResult
		return m.convertResponse(response), nil
	}

	// Make API request without beta header
	var response anthropicResponse
	err := m.provider.client.PostJSON(ctx, "/v1/messages", reqBody, &response)
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

	// Prepare headers
	headers := map[string]string{
		"Accept": "text/event-stream",
	}

	// Add beta headers if context management is enabled
	betaHeaders := m.getBetaHeaders()
	if len(betaHeaders) > 0 {
		headers["anthropic-beta"] = betaHeaders
	}

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/messages",
		Body:    reqBody,
		Headers: headers,
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Create stream wrapper
	return newAnthropicStream(httpResp.Body), nil
}

// buildRequestBody builds the Anthropic API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}

	// Convert messages (Anthropic format)
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToAnthropicMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToAnthropicMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}

	// Add system message separately (Anthropic requires this)
	if opts.Prompt.System != "" {
		body["system"] = opts.Prompt.System
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

	// Add tools if present
	if len(opts.Tools) > 0 {
		body["tools"] = ToAnthropicFormatWithCache(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToAnthropic(opts.ToolChoice)
		}
	}

	// Add context management if configured (beta feature)
	if m.options != nil && m.options.ContextManagement != nil {
		body["context_management"] = m.options.ContextManagement
	}

	return body
}

// convertResponse converts an Anthropic response to GenerateResult
// Updated in v6.0 to support detailed usage tracking and context management
func (m *LanguageModel) convertResponse(response anthropicResponse) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:       convertAnthropicUsage(response.Usage),
		RawResponse: response,
	}

	// Extract text from content blocks
	var textParts []string
	for _, content := range response.Content {
		if content.Type == "text" {
			textParts = append(textParts, content.Text)
		}
	}
	if len(textParts) > 0 {
		result.Text = textParts[0] // For now, just take first text block
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

	// Extract context management (check root level first, then usage block)
	if response.ContextManagement != nil {
		result.ContextManagement = response.ContextManagement
	} else if response.Usage.ContextManagement != nil {
		// Fallback to legacy location in usage block
		result.ContextManagement = response.Usage.ContextManagement
	}

	return result
}

// convertAnthropicUsage converts Anthropic usage to detailed Usage struct
// Implements v6.0 detailed token tracking with prompt caching and compaction support
func convertAnthropicUsage(usage anthropicUsage) types.Usage {
	var inputTokens, outputTokens int64

	// When iterations is present (compaction occurred), sum across all iterations
	// to get the true total tokens consumed/billed. The top-level input_tokens
	// and output_tokens exclude compaction iteration usage.
	if len(usage.Iterations) > 0 {
		for _, iter := range usage.Iterations {
			inputTokens += int64(iter.InputTokens)
			outputTokens += int64(iter.OutputTokens)
		}
	} else {
		inputTokens = int64(usage.InputTokens)
		outputTokens = int64(usage.OutputTokens)
	}

	cacheCreationTokens := int64(usage.CacheCreationInputTokens)
	cacheReadTokens := int64(usage.CacheReadInputTokens)

	// Calculate total input tokens (includes all cache-related tokens)
	totalInputTokens := inputTokens + cacheCreationTokens + cacheReadTokens
	totalTokens := totalInputTokens + outputTokens

	result := types.Usage{
		InputTokens:  &totalInputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Set input token details
	// Anthropic provides: input_tokens (regular), cache_creation_input_tokens (write), cache_read_input_tokens (read)
	result.InputDetails = &types.InputTokenDetails{
		NoCacheTokens:    &inputTokens,
		CacheReadTokens:  &cacheReadTokens,
		CacheWriteTokens: &cacheCreationTokens,
	}

	// Anthropic doesn't provide reasoning tokens breakdown yet
	// So we just set the total output tokens as text tokens
	result.OutputDetails = &types.OutputTokenDetails{
		TextTokens:      &outputTokens,
		ReasoningTokens: nil,
	}

	// Store raw usage for provider-specific details
	result.Raw = map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}

	if usage.CacheCreationInputTokens > 0 {
		result.Raw["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	}
	if usage.CacheReadInputTokens > 0 {
		result.Raw["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	if len(usage.Iterations) > 0 {
		result.Raw["iterations"] = usage.Iterations
	}

	return result
}

// getBetaHeaders returns the comma-separated beta headers needed for context management
func (m *LanguageModel) getBetaHeaders() string {
	if m.options == nil || m.options.ContextManagement == nil {
		return ""
	}

	var headers []string
	hasCompact := false

	// Check which edit types are present
	for _, edit := range m.options.ContextManagement.Edits {
		if _, ok := edit.(*CompactEdit); ok {
			hasCompact = true
		}
	}

	// Always add context-management header if edits are present
	if len(m.options.ContextManagement.Edits) > 0 {
		headers = append(headers, BetaHeaderContextManagement)
	}

	// Add compact header if compact edits are present
	if hasCompact {
		headers = append(headers, BetaHeaderCompact)
	}

	// Join with comma as per Anthropic API spec
	result := ""
	for i, h := range headers {
		if i > 0 {
			result += ","
		}
		result += h
	}
	return result
}

// handleError converts various errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("anthropic", 0, "", err.Error(), err)
}

// anthropicResponse represents the Anthropic API response
// Updated in v6.0 to support prompt caching and context management
type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []anthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence string             `json:"stop_sequence,omitempty"`
	Usage        anthropicUsage     `json:"usage"`
	// Root-level context management (new location - takes precedence)
	ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
}

// anthropicUsage represents Anthropic usage information with cache tracking and context management
type anthropicUsage struct {
	InputTokens              int                         `json:"input_tokens"`
	OutputTokens             int                         `json:"output_tokens"`
	CacheCreationInputTokens int                         `json:"cache_creation_input_tokens,omitempty"` // v6.0
	CacheReadInputTokens     int                         `json:"cache_read_input_tokens,omitempty"`     // v6.0
	// Legacy location for context management (fallback)
	ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
	// Iterations breakdown when compaction is used
	Iterations []UsageIteration `json:"iterations,omitempty"`
}

// UsageIteration represents a single iteration in the usage breakdown
// When compaction occurs, the API returns an iterations array showing
// usage for each sampling iteration (compaction + message).
type UsageIteration struct {
	Type         string `json:"type"`          // "compaction" or "message"
	InputTokens  int    `json:"input_tokens"`  // Input tokens for this iteration
	OutputTokens int    `json:"output_tokens"` // Output tokens for this iteration
}

// anthropicContent represents content in an Anthropic response
type anthropicContent struct {
	Type string                 `json:"type"` // "text" or "tool_use"
	Text string                 `json:"text,omitempty"`
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

// anthropicStream implements provider.TextStream for Anthropic streaming
type anthropicStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

// newAnthropicStream creates a new Anthropic stream
func newAnthropicStream(reader io.ReadCloser) *anthropicStream {
	return &anthropicStream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
	}
}

// Read implements io.Reader
func (s *anthropicStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *anthropicStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *anthropicStream) Next() (*provider.StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}

	// Get next SSE event
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}

	// Anthropic uses different event types
	switch event.Event {
	case "message_start", "content_block_start", "ping":
		// Skip these events, get next
		return s.Next()

	case "content_block_delta":
		// Parse delta
		var delta struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			return nil, fmt.Errorf("failed to parse content delta: %w", err)
		}

		if delta.Delta.Type == "text_delta" {
			return &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: delta.Delta.Text,
			}, nil
		}

	case "message_delta":
		// Parse message delta for finish reason and context management
		var delta struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
				// Legacy location for context management
				ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
				// Iterations breakdown for compaction
				Iterations []UsageIteration `json:"iterations,omitempty"`
			} `json:"usage"`
			// Root-level context management (new location - takes precedence)
			ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
		}
		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			return nil, fmt.Errorf("failed to parse message delta: %w", err)
		}

		if delta.Delta.StopReason != "" {
			var finishReason types.FinishReason
			switch delta.Delta.StopReason {
			case "end_turn":
				finishReason = types.FinishReasonStop
			case "max_tokens":
				finishReason = types.FinishReasonLength
			case "tool_use":
				finishReason = types.FinishReasonToolCalls
			default:
				finishReason = types.FinishReasonOther
			}

			chunk := &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: finishReason,
			}

			// Extract context management (check root level first, then usage block)
			if delta.ContextManagement != nil {
				chunk.ContextManagement = delta.ContextManagement
			} else if delta.Usage.ContextManagement != nil {
				chunk.ContextManagement = delta.Usage.ContextManagement
			}

			return chunk, nil
		}

	case "message_stop":
		// Stream complete
		s.err = io.EOF
		return nil, io.EOF
	}

	// Unknown event, get next
	return s.Next()
}

// Err returns any error that occurred during streaming
func (s *anthropicStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

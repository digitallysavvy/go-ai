package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
// via output_config.format. Matches the TS SDK getModelCapabilities() logic:
// claude-*-4-6, claude-*-4-5, and claude-opus-4-1 families return true.
func (m *LanguageModel) SupportsStructuredOutput() bool {
	id := m.modelID
	return strings.Contains(id, "claude-sonnet-4-6") ||
		strings.Contains(id, "claude-opus-4-6") ||
		strings.Contains(id, "claude-sonnet-4-5") ||
		strings.Contains(id, "claude-opus-4-5") ||
		strings.Contains(id, "claude-haiku-4-5") ||
		strings.Contains(id, "claude-opus-4-1")
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

	// Collect beta headers from model options and tool requirements (non-streaming)
	betaHeaders := m.combineBetaHeaders(opts, false)
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
		result := m.convertResponse(response)
		if w := m.detectSkillsWarning(opts); w != nil {
			result.Warnings = append(result.Warnings, *w)
		}
		return result, nil
	}

	// Make API request without beta header
	var response anthropicResponse
	err := m.provider.client.PostJSON(ctx, "/v1/messages", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response to GenerateResult
	result := m.convertResponse(response)
	if w := m.detectSkillsWarning(opts); w != nil {
		result.Warnings = append(result.Warnings, *w)
	}
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming enabled
	reqBody := m.buildRequestBody(opts, true)

	// Prepare headers
	headers := map[string]string{
		"Accept": "text/event-stream",
	}

	// Collect beta headers from model options and tool requirements (streaming)
	betaHeaders := m.combineBetaHeaders(opts, true)
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

	// Temperature, top_k, and top_p are incompatible with thinking mode (Anthropic API
	// rejects them). Also, top_p and temperature are mutually exclusive — only one can
	// be sent at a time. Matches TS SDK: !isThinking && (topP != null && temp == null).
	isThinking := m.options != nil && m.options.Thinking != nil &&
		m.options.Thinking.Type != ThinkingTypeDisabled
	if !isThinking {
		if opts.Temperature != nil {
			body["temperature"] = *opts.Temperature
		}
		if opts.TopK != nil {
			body["top_k"] = *opts.TopK
		}
		// top_p is only valid when temperature is not also set
		if opts.TopP != nil && opts.Temperature == nil {
			body["top_p"] = *opts.TopP
		}
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

	// disable_parallel_tool_use merges into the existing tool_choice object (or creates
	// a new one). Matches TS SDK behavior: { ...toolChoice, disable_parallel_tool_use: true }.
	if m.options != nil && m.options.DisableParallelToolUse {
		if existing, ok := body["tool_choice"]; ok {
			if tcMap, ok := existing.(map[string]interface{}); ok {
				tcMap["disable_parallel_tool_use"] = true
			}
		} else {
			body["tool_choice"] = map[string]interface{}{
				"disable_parallel_tool_use": true,
			}
		}
	}

	// Add thinking configuration if configured (beta feature)
	if m.options != nil && m.options.Thinking != nil {
		thinkingConfig := map[string]interface{}{
			"type": string(m.options.Thinking.Type),
		}
		// Only add budget_tokens for "enabled" type
		if m.options.Thinking.Type == ThinkingTypeEnabled && m.options.Thinking.BudgetTokens != nil {
			thinkingConfig["budget_tokens"] = *m.options.Thinking.BudgetTokens
		}
		body["thinking"] = thinkingConfig
	}

	// Add speed configuration if set (fast mode for Opus 4.6)
	if m.options != nil && m.options.Speed != "" {
		body["speed"] = string(m.options.Speed)
	}

	// Add context management if configured (beta feature)
	if m.options != nil && m.options.ContextManagement != nil {
		body["context_management"] = m.options.ContextManagement
	}

	// Build output_config from effort and/or ResponseFormat.
	// Both can contribute to the same output_config object.
	outputConfig := map[string]interface{}{}
	if m.options != nil && m.options.Effort != "" {
		outputConfig["effort"] = string(m.options.Effort)
	}
	if opts.ResponseFormat != nil && (opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_schema") {
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

	// cache_control: explicit CacheControl takes precedence over AutomaticCaching.
	if m.options != nil && m.options.CacheControl != nil {
		body["cache_control"] = m.options.CacheControl
	} else if m.options != nil && m.options.AutomaticCaching {
		body["cache_control"] = map[string]string{"type": "auto"}
	}

	// Add MCP servers when configured. Optional fields (authorization_token,
	// tool_configuration) are omitted when not set.
	if m.options != nil && len(m.options.MCPServers) > 0 {
		servers := make([]map[string]interface{}, len(m.options.MCPServers))
		for i, s := range m.options.MCPServers {
			srv := map[string]interface{}{
				"type": s.Type,
				"name": s.Name,
				"url":  s.URL,
			}
			if s.AuthorizationToken != "" {
				srv["authorization_token"] = s.AuthorizationToken
			}
			if s.ToolConfiguration != nil {
				tc := map[string]interface{}{}
				if len(s.ToolConfiguration.AllowedTools) > 0 {
					tc["allowed_tools"] = s.ToolConfiguration.AllowedTools
				}
				if s.ToolConfiguration.Enabled != nil {
					tc["enabled"] = *s.ToolConfiguration.Enabled
				}
				if len(tc) > 0 {
					srv["tool_configuration"] = tc
				}
			}
			servers[i] = srv
		}
		body["mcp_servers"] = servers
	}

	// Add container config. ContainerID (string shorthand) takes precedence over Container struct.
	// When Container has skills, send as an object {id, skills}; otherwise send the plain ID string.
	// This matches the TypeScript SDK behavior.
	if m.options != nil && m.options.ContainerID != "" {
		body["container"] = m.options.ContainerID
	} else if m.options != nil && m.options.Container != nil {
		if len(m.options.Container.Skills) > 0 {
			// Object format when skills are provided (agent skills feature)
			containerBody := map[string]interface{}{}
			if m.options.Container.ID != "" {
				containerBody["id"] = m.options.Container.ID
			}
			skills := make([]map[string]interface{}, len(m.options.Container.Skills))
			for i, s := range m.options.Container.Skills {
				skill := map[string]interface{}{
					"type":     s.Type,
					"skill_id": s.SkillID,
				}
				if s.Version != "" {
					skill["version"] = s.Version
				}
				skills[i] = skill
			}
			containerBody["skills"] = skills
			body["container"] = containerBody
		} else if m.options.Container.ID != "" {
			// String format when no skills (referencing/resuming an existing container)
			body["container"] = m.options.Container.ID
		}
		// Otherwise (empty ContainerConfig): don't add any container field
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

	// Extract tool calls (regular and MCP)
	for _, content := range response.Content {
		switch content.Type {
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:        content.ID,
				ToolName:  content.Name,
				Arguments: content.Input,
			})
		case "mcp_tool_use":
			// MCP tool calls are executed server-side; surface them as ToolCalls
			// so callers can inspect which MCP tools the model invoked.
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

// Tool name constants used to detect code execution tools when building beta headers and warnings.
// Defined here to avoid importing the tools sub-package.
const (
	codeExecution20260120ToolName = "anthropic.code_execution_20260120"
	codeExecution20250825ToolName = "anthropic.code_execution_20250825"
)

// combineBetaHeaders combines model-option beta headers with any request-specific
// beta headers. stream should be true when called from DoStream.
func (m *LanguageModel) combineBetaHeaders(opts *provider.GenerateOptions, stream bool) string {
	base := m.getBetaHeaders()

	// Fine-grained tool streaming: always on by default during streaming.
	// Disabled only when ToolStreaming is explicitly set to false.
	if stream {
		toolStreamingEnabled := true
		if m.options != nil && m.options.ToolStreaming != nil {
			toolStreamingEnabled = *m.options.ToolStreaming
		}
		if toolStreamingEnabled {
			if base != "" {
				base += "," + BetaHeaderFineGrainedToolStreaming
			} else {
				base = BetaHeaderFineGrainedToolStreaming
			}
		}
	}

	// Detect code execution tool and inject its required beta header
	if opts != nil {
		for _, t := range opts.Tools {
			if t.Name == codeExecution20260120ToolName {
				if base != "" {
					base += "," + BetaHeaderCodeExecution
				} else {
					base = BetaHeaderCodeExecution
				}
				break
			}
		}
	}

	return base
}

// getBetaHeaders returns the comma-separated beta headers needed for context management
func (m *LanguageModel) getBetaHeaders() string {
	if m.options == nil {
		return ""
	}

	var headers []string

	// Check context management for beta headers
	if m.options.ContextManagement != nil {
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
	}

	// Add fast mode header if fast mode is enabled
	if m.options.Speed == SpeedFast {
		headers = append(headers, BetaHeaderFastMode)
	}

	// Add automatic caching beta header when automatic caching is enabled
	if m.options.AutomaticCaching {
		headers = append(headers, BetaHeaderPromptCaching)
	}

	// Add effort beta header when effort level is set
	if m.options.Effort != "" {
		headers = append(headers, BetaHeaderEffort)
	}

	// Add MCP client beta header when MCP servers are configured
	if len(m.options.MCPServers) > 0 {
		headers = append(headers, BetaHeaderMCPClient)
	}

	// Container skills require three beta headers; plain container without skills needs none
	if m.options.Container != nil && len(m.options.Container.Skills) > 0 {
		headers = append(headers,
			BetaHeaderCodeExecution20250825,
			BetaHeaderSkills,
			BetaHeaderFilesAPI,
		)
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

// detectSkillsWarning returns a warning when container skills are configured but no code
// execution tool is present in opts. Matches TypeScript SDK behavior.
func (m *LanguageModel) detectSkillsWarning(opts *provider.GenerateOptions) *types.Warning {
	if m.options == nil || m.options.Container == nil || len(m.options.Container.Skills) == 0 {
		return nil
	}
	if opts != nil {
		for _, t := range opts.Tools {
			if t.Name == codeExecution20250825ToolName || t.Name == codeExecution20260120ToolName {
				return nil
			}
		}
	}
	return &types.Warning{
		Type:    "other",
		Message: "code execution tool is required when using skills",
	}
}

// handleError converts various errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("anthropic", 0, "", err.Error(), err)
}

// anthropicContainerResponse represents container info returned in the Anthropic API response.
// The container field is present when a container was used or created during the request.
type anthropicContainerResponse struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at,omitempty"`
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
	// Container info returned when a container was used/created
	Container *anthropicContainerResponse `json:"container,omitempty"`
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
	Type      string                 `json:"type"` // "text", "tool_use", "thinking", "redacted_thinking"
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Thinking  string                 `json:"thinking,omitempty"`  // For "thinking" type
	Signature string                 `json:"signature,omitempty"` // For "thinking" type
	Data      string                 `json:"data,omitempty"`      // For "redacted_thinking" type
}

// streamContentBlock tracks an in-flight content block across SSE events.
// A block is opened by content_block_start and closed by content_block_stop.
type streamContentBlock struct {
	blockType        string          // "text", "tool-call", "reasoning"
	toolCallID       string          // for tool-call blocks (content_block.id)
	toolName         string          // for tool-call blocks (display name emitted in chunk)
	providerToolName string          // original Anthropic provider name (e.g. "bash_code_execution")
	inputBuf         strings.Builder // accumulates input_json_delta fragments
	firstDelta       bool            // true until the first input_json_delta is consumed
}

// anthropicStream implements provider.TextStream for Anthropic streaming
type anthropicStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
	// Input token counts captured from the message_start event.
	// These are combined with output tokens when emitting the finish chunk.
	inputTokens      int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	// In-flight content blocks, keyed by SSE index.
	// Populated by content_block_start; removed on content_block_stop.
	contentBlocks map[int]*streamContentBlock
	// pending holds chunks assembled outside the normal one-event-per-call
	// flow (e.g. pre-populated tool calls from message_start.message.content).
	// They are drained before the next SSE event is read.
	pending []*provider.StreamChunk
	// container info parsed from message_start/message_delta events.
	// Present when the model used or created a container during the request.
	container *anthropicContainerResponse
}

// newAnthropicStream creates a new Anthropic stream
func newAnthropicStream(reader io.ReadCloser) *anthropicStream {
	return &anthropicStream{
		reader:        reader,
		parser:        streaming.NewSSEParser(reader),
		contentBlocks: make(map[int]*streamContentBlock),
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

	// Drain any chunks buffered by a previous event (e.g. pre-populated tool
	// calls from message_start) before reading the next SSE event.
	if len(s.pending) > 0 {
		chunk := s.pending[0]
		s.pending = s.pending[1:]
		return chunk, nil
	}

	// Get next SSE event
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}

	// Anthropic uses different event types
	switch event.Event {
	case "ping":
		// No-op: keep alive signal, get next chunk
		return s.Next()

	case "content_block_start":
		// Parse the opening of a content block. Store tool_use, server_tool_use,
		// and thinking blocks in s.contentBlocks for later accumulation/emission.
		var start struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type  string                 `json:"type"`
				ID    string                 `json:"id"`
				Name  string                 `json:"name"`
				Input map[string]interface{} `json:"input"` // non-empty for programmatic deferred tool calls
				// mcp_tool_use fields
				ServerName string `json:"server_name"`
				// mcp_tool_result fields
				ToolUseID string      `json:"tool_use_id"`
				IsError   bool        `json:"is_error"`
				Content   interface{} `json:"content"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(event.Data), &start); err != nil {
			// Malformed start event: skip gracefully
			return s.Next()
		}
		switch start.ContentBlock.Type {
		case "tool_use":
			// Some deferred (programmatic) tool calls carry their full input
			// directly in content_block_start rather than via input_json_delta.
			// Serialize it as the initial buffer content so content_block_stop
			// emits the correct arguments even with no following deltas.
			var initialInput string
			if len(start.ContentBlock.Input) > 0 {
				if b, err := json.Marshal(start.ContentBlock.Input); err == nil {
					initialInput = string(b)
				}
			}
			block := &streamContentBlock{
				blockType:  "tool-call",
				toolCallID: start.ContentBlock.ID,
				toolName:   start.ContentBlock.Name,
				firstDelta: initialInput == "", // expect deltas only when no initial input
			}
			if initialInput != "" {
				block.inputBuf.WriteString(initialInput)
			}
			s.contentBlocks[start.Index] = block

		case "thinking":
			s.contentBlocks[start.Index] = &streamContentBlock{
				blockType: "reasoning",
			}

		case "redacted_thinking":
			// Treat redacted thinking blocks the same as thinking: they carry
			// reasoning content whose text has been redacted for safety reasons.
			// We cannot surface the redacted data without a providerMetadata field,
			// but we track the block so content_block_stop is a clean no-op.
			s.contentBlocks[start.Index] = &streamContentBlock{
				blockType: "reasoning",
			}

		case "server_tool_use":
			// Provider-executed tools: web_fetch, web_search, code_execution,
			// bash_code_execution, text_editor_code_execution.
			// bash/text_editor variants are normalized to "code_execution" for
			// the emitted tool name, but the original name is stored so the
			// first-delta type prefix can be injected (see input_json_delta).
			toolName := start.ContentBlock.Name
			providerToolName := start.ContentBlock.Name
			if toolName == "bash_code_execution" || toolName == "text_editor_code_execution" {
				toolName = "code_execution"
			}
			s.contentBlocks[start.Index] = &streamContentBlock{
				blockType:        "tool-call",
				toolCallID:       start.ContentBlock.ID,
				toolName:         toolName,
				providerToolName: providerToolName,
				firstDelta:       true,
			}

		case "mcp_tool_use":
			// MCP tool calls have their full input pre-populated in content_block_start.
			// Emit immediately as a tool call chunk (no input_json_delta accumulation needed).
			input := start.ContentBlock.Input
			if input == nil {
				input = map[string]interface{}{}
			}
			// Track as a non-buffering block so content_block_stop is a clean no-op.
			s.contentBlocks[start.Index] = &streamContentBlock{
				blockType: "mcp-tool-use",
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:        start.ContentBlock.ID,
					ToolName:  start.ContentBlock.Name,
					Arguments: input,
				},
			}, nil

		case "mcp_tool_result":
			// MCP tool results arrive in content_block_start but there is no
			// ToolResult chunk type in the Go stream API. Track the block so
			// content_block_stop is a clean no-op.
			s.contentBlocks[start.Index] = &streamContentBlock{
				blockType: "mcp-tool-result",
			}
			return s.Next()

		default:
			// "text", "compaction", and any unknown types: record so
			// content_block_stop is always a clean no-op.
			s.contentBlocks[start.Index] = &streamContentBlock{
				blockType: start.ContentBlock.Type,
			}
		}
		return s.Next()

	case "message_start":
		// Capture input/cache tokens for inclusion in the final finish chunk.
		// These are only available here — the message_delta only has output_tokens.
		//
		// Also handle pre-populated tool_use content blocks (programmatic /
		// deferred tool calling). In this pattern the tool call input arrives
		// in message_start.message.content rather than via content_block_delta
		// events, so we emit ChunkTypeToolCall for each such block immediately.
		var msg struct {
			Message struct {
				Usage struct {
					InputTokens              int `json:"input_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
				Container *anthropicContainerResponse `json:"container,omitempty"`
				Content   []struct {
					Type  string                 `json:"type"`
					ID    string                 `json:"id"`
					Name  string                 `json:"name"`
					Input map[string]interface{} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(event.Data), &msg); err == nil {
			s.inputTokens = int64(msg.Message.Usage.InputTokens)
			s.cacheReadTokens = int64(msg.Message.Usage.CacheReadInputTokens)
			s.cacheWriteTokens = int64(msg.Message.Usage.CacheCreationInputTokens)

			// Capture container info (present when container was used/created).
			// In message_start it contains id and expires_at but no skills.
			if msg.Message.Container != nil {
				s.container = msg.Message.Container
			}

			// Buffer a ChunkTypeToolCall for each pre-populated tool_use block.
			for _, part := range msg.Message.Content {
				if part.Type != "tool_use" {
					continue
				}
				args := part.Input
				if args == nil {
					args = map[string]interface{}{}
				}
				s.pending = append(s.pending, &provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:        part.ID,
						ToolName:  part.Name,
						Arguments: args,
					},
				})
			}
		}
		return s.Next()

	case "content_block_delta":
		// Parse delta — content is *string to allow null in compaction_delta events.
		// PartialJSON accumulates tool call argument fragments.
		// Thinking carries reasoning text deltas.
		var delta struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type        string  `json:"type"`
				Text        string  `json:"text"`
				Content     *string `json:"content"`      // nullable in compaction_delta
				PartialJSON string  `json:"partial_json"` // in input_json_delta
				Thinking    string  `json:"thinking"`     // in thinking_delta
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			return nil, fmt.Errorf("failed to parse content delta: %w", err)
		}

		switch delta.Delta.Type {
		case "text_delta":
			return &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: delta.Delta.Text,
			}, nil

		case "input_json_delta":
			// Skip empty deltas — the TS SDK does the same to allow replacing
			// the first character in code-execution tools without double-writing.
			if delta.Delta.PartialJSON == "" {
				return s.Next()
			}
			if block := s.contentBlocks[delta.Index]; block != nil {
				partialJSON := delta.Delta.PartialJSON
				// For bash_code_execution and text_editor_code_execution the API
				// streams raw arguments without a type discriminator. On the first
				// delta, inject {"type":"<providerToolName>", so that the assembled
				// JSON can be decoded as a CodeExecutionInput union value.
				if block.firstDelta && (block.providerToolName == "bash_code_execution" ||
					block.providerToolName == "text_editor_code_execution") &&
					len(partialJSON) > 0 && partialJSON[0] == '{' {
					partialJSON = `{"type":"` + block.providerToolName + `",` + partialJSON[1:]
				}
				block.firstDelta = false
				block.inputBuf.WriteString(partialJSON)
			}
			return s.Next()

		case "thinking_delta":
			// Emit each thinking fragment immediately as a reasoning chunk.
			return &provider.StreamChunk{
				Type:      provider.ChunkTypeReasoning,
				Reasoning: delta.Delta.Thinking,
			}, nil

		case "signature_delta":
			// Thinking block signature: cryptographic attestation, not user-visible.
			return s.Next()

		case "compaction_delta":
			// Emit non-null content as text; skip null content.
			if delta.Delta.Content != nil {
				return &provider.StreamChunk{
					Type: provider.ChunkTypeText,
					Text: *delta.Delta.Content,
				}, nil
			}
			return s.Next()
		}

	case "content_block_stop":
		// A content block has been fully delivered. For tool-call blocks, emit the
		// assembled ChunkTypeToolCall with the complete JSON-parsed arguments.
		// For all other block types this is a clean no-op.
		var stop struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(event.Data), &stop); err != nil {
			return s.Next()
		}
		block := s.contentBlocks[stop.Index]
		delete(s.contentBlocks, stop.Index)

		if block != nil && block.blockType == "tool-call" {
			// Parse the accumulated JSON into ToolCall.Arguments.
			var args map[string]interface{}
			inputStr := block.inputBuf.String()
			if inputStr != "" {
				if err := json.Unmarshal([]byte(inputStr), &args); err != nil {
					// Malformed JSON from the API: surface as error.
					return nil, fmt.Errorf("failed to parse tool call arguments for %q: %w", block.toolName, err)
				}
			}
			if args == nil {
				args = map[string]interface{}{}
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:        block.toolCallID,
					ToolName:  block.toolName,
					Arguments: args,
				},
			}, nil
		}
		// Non-tool block (text, reasoning, etc.) — no chunk to emit.
		return s.Next()

	case "message_delta":
		// Parse message delta for finish reason, context management, and container.
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
			// Container info (with skills, if any) from message_delta
			Container *anthropicContainerResponse `json:"container,omitempty"`
		}
		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			return nil, fmt.Errorf("failed to parse message delta: %w", err)
		}

		// Update container state if the delta contains container info (includes skills).
		if delta.Container != nil {
			s.container = delta.Container
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

			// Build usage from tokens captured across message_start and message_delta.
			outputTokens := int64(delta.Usage.OutputTokens)
			inputTotal := s.inputTokens + s.cacheReadTokens + s.cacheWriteTokens
			totalTokens := inputTotal + outputTokens
			usage := &types.Usage{
				InputTokens:  &inputTotal,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
				InputDetails: &types.InputTokenDetails{
					NoCacheTokens:    &s.inputTokens,
					CacheReadTokens:  &s.cacheReadTokens,
					CacheWriteTokens: &s.cacheWriteTokens,
				},
			}

			chunk := &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: finishReason,
				Usage:        usage,
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

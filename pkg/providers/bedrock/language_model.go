package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LanguageModel implements the provider.LanguageModel interface for AWS Bedrock
type LanguageModel struct {
	provider *Provider
	modelID  string
	options  *ModelOptions
}

// NewLanguageModel creates a new AWS Bedrock language model
func NewLanguageModel(provider *Provider, modelID string, options ...*ModelOptions) *LanguageModel {
	var opts *ModelOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
		options:  opts,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "aws-bedrock"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Claude models on Bedrock support tools
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Claude models on Bedrock support vision
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine the model provider (Claude, Llama, etc.)
	endpoint := m.getInvokeEndpoint()

	// Create HTTP request
	url := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com%s", m.provider.config.Region, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Sign the request with AWS Signature V4
	signer := NewAWSSigner(
		m.provider.config.AWSAccessKeyID,
		m.provider.config.AWSSecretAccessKey,
		m.provider.config.SessionToken,
		m.provider.config.Region,
	)

	if err := signer.SignRequest(req, bodyBytes); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError("aws-bedrock", 0, "", err.Error(), err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("AWS Bedrock API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return m.convertResponse(respBody)
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// For simplicity, simulate streaming by chunking the non-streaming response
	result, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}

	stream := &bedrockStream{
		result:   result,
		position: 0,
		done:     false,
	}

	return stream, nil
}

func (m *LanguageModel) getInvokeEndpoint() string {
	// Bedrock model invocation endpoint
	return fmt.Sprintf("/model/%s/invoke", m.modelID)
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	// Different Bedrock models have different request formats
	// We'll support Claude models (most common on Bedrock)
	if strings.Contains(m.modelID, "anthropic") || strings.Contains(m.modelID, "claude") {
		return m.buildClaudeRequest(opts)
	}

	// Default format for other models
	return m.buildGenericRequest(opts)
}

func (m *LanguageModel) buildClaudeRequest(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	messages := []map[string]interface{}{}

	if opts.Prompt.IsMessages() {
		for _, msg := range opts.Prompt.Messages {
			content := ""
			for _, c := range msg.Content {
				if tc, ok := c.(types.TextContent); ok {
					content += tc.Text
				}
			}

			messages = append(messages, map[string]interface{}{
				"role":    msg.Role,
				"content": content,
			})
		}
	} else if opts.Prompt.IsSimple() {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": opts.Prompt.Text,
		})
	}

	reqBody := map[string]interface{}{
		"messages":      messages,
		"anthropic_version": "bedrock-2023-05-31",
	}

	if opts.MaxTokens != nil {
		reqBody["max_tokens"] = *opts.MaxTokens
	} else {
		reqBody["max_tokens"] = 4096 // Claude requires max_tokens
	}

	if opts.Temperature != nil {
		reqBody["temperature"] = *opts.Temperature
	}

	if opts.TopP != nil {
		reqBody["top_p"] = *opts.TopP
	}

	// Add thinking configuration if configured
	if m.options != nil && m.options.Thinking != nil {
		thinkingConfig := map[string]interface{}{
			"type": string(m.options.Thinking.Type),
		}
		// Only add budget_tokens for "enabled" type
		if m.options.Thinking.Type == ThinkingTypeEnabled && m.options.Thinking.BudgetTokens != nil {
			thinkingConfig["budget_tokens"] = *m.options.Thinking.BudgetTokens
		}
		reqBody["thinking"] = thinkingConfig
	}

	// Add tools and tool choice (#12893 strict mode, #12854 tool choice enforcement).
	// When toolChoice is "none" the tools array is cleared and no toolChoice is sent.
	if len(opts.Tools) > 0 && opts.ToolChoice.Type != types.ToolChoiceNone {
		// Filter to the single requested tool when a specific tool is named.
		toolList := opts.Tools
		if opts.ToolChoice.Type == types.ToolChoiceTool && opts.ToolChoice.ToolName != "" {
			filtered := make([]types.Tool, 0, 1)
			for _, t := range opts.Tools {
				if t.Name == opts.ToolChoice.ToolName {
					filtered = append(filtered, t)
				}
			}
			toolList = filtered
		}

		bedrockTools := make([]interface{}, 0, len(toolList))
		for _, t := range toolList {
			toolSpec := map[string]interface{}{
				"name": t.Name,
			}
			if t.Description != "" {
				toolSpec["description"] = t.Description
			}
			if t.Strict {
				toolSpec["strict"] = true
			}
			var inputSchema interface{}
			if t.Parameters != nil {
				inputSchema = t.Parameters
			} else {
				inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			toolSpec["inputSchema"] = map[string]interface{}{"json": inputSchema}
			bedrockTools = append(bedrockTools, map[string]interface{}{"toolSpec": toolSpec})
		}
		reqBody["tools"] = bedrockTools

		// Map SDK tool choice to Bedrock's format.
		switch opts.ToolChoice.Type {
		case types.ToolChoiceAuto:
			reqBody["toolChoice"] = map[string]interface{}{"auto": map[string]interface{}{}}
		case types.ToolChoiceRequired:
			reqBody["toolChoice"] = map[string]interface{}{"any": map[string]interface{}{}}
		case types.ToolChoiceTool:
			reqBody["toolChoice"] = map[string]interface{}{
				"tool": map[string]interface{}{"name": opts.ToolChoice.ToolName},
			}
		}
	}

	return reqBody, nil
}

func (m *LanguageModel) buildGenericRequest(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	var prompt string
	if opts.Prompt.IsMessages() {
		for _, msg := range opts.Prompt.Messages {
			content := ""
			for _, c := range msg.Content {
				if tc, ok := c.(types.TextContent); ok {
					content += tc.Text
				}
			}
			prompt += fmt.Sprintf("%s: %s\n", msg.Role, content)
		}
	} else if opts.Prompt.IsSimple() {
		prompt = opts.Prompt.Text
	}

	reqBody := map[string]interface{}{
		"prompt": prompt,
	}

	if opts.MaxTokens != nil {
		reqBody["max_tokens"] = *opts.MaxTokens
	}

	if opts.Temperature != nil {
		reqBody["temperature"] = *opts.Temperature
	}

	return reqBody, nil
}

// bedrockUsage represents Bedrock usage information with detailed token tracking
type bedrockUsage struct {
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	TotalTokens           int `json:"total_tokens,omitempty"`
	CacheReadInputTokens  int `json:"cache_read_input_tokens,omitempty"`  // v6.0
	CacheWriteInputTokens int `json:"cache_creation_input_tokens,omitempty"` // v6.0
}

// convertBedrockUsage converts Bedrock usage to detailed Usage struct
// Implements v6.0 detailed token tracking with cache support
// Bedrock supports BOTH cache read and cache write tokens
func convertBedrockUsage(usage bedrockUsage) types.Usage {
	inputTokens := int64(usage.InputTokens)
	outputTokens := int64(usage.OutputTokens)
	cacheReadTokens := int64(usage.CacheReadInputTokens)
	cacheWriteTokens := int64(usage.CacheWriteInputTokens)

	// Calculate totals
	totalInputTokens := inputTokens
	totalTokens := totalInputTokens + outputTokens

	result := types.Usage{
		InputTokens:  &totalInputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Set input token details (Bedrock provides BOTH cache read and write)
	if cacheReadTokens > 0 || cacheWriteTokens > 0 {
		noCacheTokens := inputTokens - cacheReadTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cacheReadTokens,
			CacheWriteTokens: &cacheWriteTokens,
		}
	}

	// Bedrock doesn't provide reasoning tokens breakdown yet
	result.OutputDetails = &types.OutputTokenDetails{
		TextTokens:      &outputTokens,
		ReasoningTokens: nil,
	}

	// Store raw usage
	result.Raw = map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}

	if usage.TotalTokens > 0 {
		result.Raw["total_tokens"] = usage.TotalTokens
	}
	if usage.CacheReadInputTokens > 0 {
		result.Raw["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	if usage.CacheWriteInputTokens > 0 {
		result.Raw["cache_creation_input_tokens"] = usage.CacheWriteInputTokens
	}

	return result
}

func (m *LanguageModel) convertResponse(body []byte) (*types.GenerateResult, error) {
	// Try Claude response format
	// Updated in v6.0 to support detailed usage tracking
	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string       `json:"stop_reason"`
		Usage      bedrockUsage `json:"usage"`
	}

	if err := json.Unmarshal(body, &claudeResp); err == nil && len(claudeResp.Content) > 0 {
		text := ""
		for _, content := range claudeResp.Content {
			if content.Type == "text" {
				text += content.Text
			}
		}

		finishReason := types.FinishReasonStop
		if claudeResp.StopReason == "max_tokens" {
			finishReason = types.FinishReasonLength
		}

		return &types.GenerateResult{
			Text:         text,
			FinishReason: finishReason,
			Usage:        convertBedrockUsage(claudeResp.Usage),
			RawResponse:  claudeResp,
		}, nil
	}

	// Try generic response format
	var genericResp struct {
		Completion string `json:"completion"`
		Generation string `json:"generation"`
	}

	if err := json.Unmarshal(body, &genericResp); err == nil {
		text := genericResp.Completion
		if text == "" {
			text = genericResp.Generation
		}

		// Generic format doesn't provide usage information
		return &types.GenerateResult{
			Text:         text,
			FinishReason: types.FinishReasonStop,
			Usage:        types.Usage{}, // Empty usage
			RawResponse:  genericResp,
		}, nil
	}

	return nil, fmt.Errorf("unexpected response format from Bedrock: %s", string(body))
}

type bedrockStream struct {
	result   *types.GenerateResult
	position int
	done     bool
}

func (s *bedrockStream) Next() (*provider.StreamChunk, error) {
	if s.done {
		return nil, fmt.Errorf("stream exhausted")
	}

	chunkSize := 10
	text := s.result.Text

	if s.position >= len(text) {
		s.done = true
		return &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			Text:         "",
			FinishReason: s.result.FinishReason,
			Usage:        &s.result.Usage,
		}, nil
	}

	end := s.position + chunkSize
	if end > len(text) {
		end = len(text)
	}

	chunk := text[s.position:end]
	s.position = end

	return &provider.StreamChunk{
		Type: provider.ChunkTypeText,
		Text: chunk,
	}, nil
}

func (s *bedrockStream) Err() error {
	return nil
}

func (s *bedrockStream) Close() error {
	s.done = true
	return nil
}

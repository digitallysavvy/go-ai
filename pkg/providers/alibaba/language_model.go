package alibaba

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements the provider.LanguageModel interface for Alibaba Qwen models
type LanguageModel struct {
	prov    *Provider
	modelID string
}

// NewLanguageModel creates a new Alibaba language model
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
	return "alibaba"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return true // Qwen models support OpenAI-compatible tool calling
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true // Qwen models support JSON mode
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Only qwen-vl-max supports image inputs
	return m.modelID == "qwen-vl-max"
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody := m.buildRequestBody(opts, false)

	var response alibabaResponse
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
		Method: "POST",
		Path:   "/chat/completions",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Check for HTTP errors
	if httpResp.StatusCode != 200 {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("API returned status %d: %s", httpResp.StatusCode, string(body))
	}

	// Create stream wrapper
	return newAlibabaStream(httpResp.Body), nil
}

// buildRequestBody builds the API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}

	// Extract Alibaba-level cache control (applies to all message types).
	// When enabled, a CacheControlValidator enforces the 4-breakpoint limit and
	// accumulates warnings when the limit is exceeded.
	var validator *CacheControlValidator
	if opts.ProviderOptions != nil {
		if alibabaOpts, ok := opts.ProviderOptions["alibaba"].(map[string]interface{}); ok {
			if cc, ok := alibabaOpts["cacheControl"]; ok && cc != nil {
				validator = NewCacheControlValidator()
			}
		}
	}

	// Convert prompt to messages with optional cache control support
	var allMessages []map[string]interface{}

	if opts.Prompt.System != "" {
		// Prepend system message (with cache control if set)
		allMessages = append(allMessages,
			ConvertToAlibabaChatMessages([]types.Message{
				{
					Role:    types.RoleSystem,
					Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}},
				},
			}, validator)...,
		)
	}

	if opts.Prompt.IsMessages() {
		allMessages = append(allMessages,
			ConvertToAlibabaChatMessages(opts.Prompt.Messages, validator)...,
		)
	} else if opts.Prompt.IsSimple() {
		allMessages = append(allMessages,
			prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))...,
		)
	}

	body["messages"] = allMessages

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

	if opts.TopK != nil {
		body["top_k"] = *opts.TopK
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

	// Alibaba-specific options from provider metadata
	if opts.ProviderOptions != nil {
		if alibabaOpts, ok := opts.ProviderOptions["alibaba"].(map[string]interface{}); ok {
			// Thinking/reasoning support
			if enableThinking, ok := alibabaOpts["enable_thinking"].(bool); ok {
				body["enable_thinking"] = enableThinking
			}
			if thinkingBudget, ok := alibabaOpts["thinking_budget"].(int); ok {
				body["thinking_budget"] = thinkingBudget
			}

			// Parallel tool calls
			if parallelToolCalls, ok := alibabaOpts["parallel_tool_calls"].(bool); ok {
				body["parallel_tool_calls"] = parallelToolCalls
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

// convertResponse converts Alibaba API response to SDK format
func (m *LanguageModel) convertResponse(resp alibabaResponse) *types.GenerateResult {
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
		Usage:        ConvertAlibabaUsage(resp.Usage),
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
	return providererrors.NewProviderError("alibaba", 0, "", err.Error(), err)
}


// alibabaResponse represents the response from Alibaba chat API
type alibabaResponse struct {
	ID                string          `json:"id"`
	Object            string          `json:"object"`
	Created           int64           `json:"created"`
	Model             string          `json:"model"`
	SystemFingerprint string          `json:"system_fingerprint,omitempty"`
	Choices           []alibabaChoice `json:"choices"`
	Usage             AlibabaUsage    `json:"usage"`
}

// alibabaChoice represents a choice in the chat response
type alibabaChoice struct {
	Index        int            `json:"index"`
	Message      alibabaMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

// alibabaMessage represents a message in the chat
type alibabaMessage struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []alibabaToolCall `json:"tool_calls,omitempty"`
}

// alibabaToolCall represents a tool call in the response
type alibabaToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function alibabaToolCallFunction `json:"function"`
}

// alibabaToolCallFunction represents a tool call function
type alibabaToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

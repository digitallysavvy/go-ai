package azure

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

// LanguageModel implements the provider.LanguageModel interface for Azure OpenAI
type LanguageModel struct {
	provider     *Provider
	deploymentID string
}

// NewLanguageModel creates a new Azure OpenAI language model
func NewLanguageModel(provider *Provider, deploymentID string) *LanguageModel {
	return &LanguageModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "azure-openai"
}

// ModelID returns the deployment ID (Azure's equivalent of model ID)
func (m *LanguageModel) ModelID() string {
	return m.deploymentID
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
	// Vision support depends on the deployed model
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts, false)

	// Make API request to Azure-specific endpoint
	path := fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=%s",
		m.deploymentID, m.provider.APIVersion())

	var response azureResponse
	err := m.provider.client.PostJSON(ctx, path, reqBody, &response)
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

	// Make streaming API request to Azure-specific endpoint
	path := fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=%s",
		m.deploymentID, m.provider.APIVersion())

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Create stream wrapper
	return newAzureStream(httpResp.Body), nil
}

// buildRequestBody builds the Azure OpenAI API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
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
	if opts.StopSequences != nil && len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// Add tools if present
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)

		// Add tool_choice if specified
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}

	// Add response format if specified
	if opts.ResponseFormat != nil {
		body["response_format"] = map[string]interface{}{
			"type": opts.ResponseFormat.Type,
		}
	}

	return body
}

// convertResponse converts an Azure OpenAI response to GenerateResult
func (m *LanguageModel) convertResponse(response azureResponse) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
		}
	}

	choice := response.Choices[0]
	result := &types.GenerateResult{
		Text:         choice.Message.Content,
		FinishReason: convertFinishReason(choice.FinishReason),
		Usage: types.Usage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
			TotalTokens:  response.Usage.TotalTokens,
		},
	}

	// Add tool calls if present
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
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

// handleError converts errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("azure-openai", 0, "", err.Error(), err)
}

// convertFinishReason converts Azure OpenAI finish reasons to our types
func convertFinishReason(reason string) types.FinishReason {
	switch reason {
	case "stop":
		return types.FinishReasonStop
	case "length":
		return types.FinishReasonLength
	case "tool_calls", "function_call":
		return types.FinishReasonToolCalls
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		return types.FinishReasonOther
	}
}

// Azure OpenAI response types (same as OpenAI)
type azureResponse struct {
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
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type azureStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// azureStream implements provider.TextStream for Azure OpenAI streaming
type azureStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

// newAzureStream creates a new Azure OpenAI stream
func newAzureStream(reader io.ReadCloser) *azureStream {
	return &azureStream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
	}
}

// Read implements io.Reader
func (s *azureStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *azureStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *azureStream) Next() (*provider.StreamChunk, error) {
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
	var chunkData azureStreamChunk
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
			tc := choice.Delta.ToolCalls[0]
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:        tc.ID,
					ToolName:  tc.Function.Name,
					Arguments: args,
				},
			}, nil
		}

		// Finish chunk
		if choice.FinishReason != "" {
			return &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: convertFinishReason(choice.FinishReason),
			}, nil
		}
	}

	// Empty chunk, get next
	return s.Next()
}

// Err returns any error that occurred during streaming
func (s *azureStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

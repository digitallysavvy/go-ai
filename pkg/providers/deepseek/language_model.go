package deepseek

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

// LanguageModel implements the provider.LanguageModel interface for Deepseek
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Deepseek language model
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
	return "deepseek"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
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
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody := m.buildRequestBody(opts, false)
	var response deepseekResponse
	err := m.provider.client.PostJSON(ctx, "/v1/chat/completions", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	return m.convertResponse(response), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody := m.buildRequestBody(opts, true)
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	return newDeepseekStream(httpResp.Body), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
	}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": opts.Prompt.System,
		}
		body["messages"] = append([]map[string]interface{}{systemMsg}, messages...)
	}
	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.StopSequences != nil && len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	if opts.ResponseFormat != nil {
		body["response_format"] = map[string]interface{}{
			"type": opts.ResponseFormat.Type,
		}
	}
	return body
}

func (m *LanguageModel) convertResponse(response deepseekResponse) *types.GenerateResult {
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
		Usage:        convertDeepseekUsage(response.Usage),
		RawResponse:  response,
	}
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

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("deepseek", 0, "", err.Error(), err)
}

func convertDeepseekUsage(usage deepseekUsage) types.Usage {
	p, c, t := int64(usage.PromptTokens), int64(usage.CompletionTokens), int64(usage.TotalTokens)
	result := types.Usage{InputTokens: &p, OutputTokens: &c, TotalTokens: &t}
	var cached int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cached = int64(*usage.PromptTokensDetails.CachedTokens)
	}
	var textTokens *int64
	var imageTokens *int64
	if usage.PromptTokensDetails != nil {
		if usage.PromptTokensDetails.TextTokens != nil {
			textVal := int64(*usage.PromptTokensDetails.TextTokens)
			textTokens = &textVal
		}
		if usage.PromptTokensDetails.ImageTokens != nil {
			imageVal := int64(*usage.PromptTokensDetails.ImageTokens)
			imageTokens = &imageVal
		}
	}
	var reasoning int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}
	if cached > 0 || textTokens != nil || imageTokens != nil {
		noCache := p - cached
		result.InputDetails = &types.InputTokenDetails{NoCacheTokens: &noCache, CacheReadTokens: &cached, CacheWriteTokens: nil, TextTokens: textTokens, ImageTokens: imageTokens}
	}
	if reasoning > 0 {
		text := c - reasoning
		result.OutputDetails = &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning}
	}
	result.Raw = map[string]interface{}{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens}
	if usage.PromptTokensDetails != nil {
		result.Raw["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokensDetails != nil {
		result.Raw["completion_tokens_details"] = usage.CompletionTokensDetails
	}
	return result
}

func convertFinishReason(reason string) types.FinishReason {
	switch reason {
	case "stop":
		return types.FinishReasonStop
	case "length":
		return types.FinishReasonLength
	case "tool_calls":
		return types.FinishReasonToolCalls
	default:
		return types.FinishReasonOther
	}
}

type deepseekResponse struct {
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
	Usage deepseekUsage `json:"usage"`
}

type deepseekUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
		AudioTokens  *int `json:"audio_tokens,omitempty"`
		TextTokens   *int `json:"text_tokens,omitempty"`
		ImageTokens  *int `json:"image_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type deepseekStreamChunk struct {
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

type deepseekStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

func newDeepseekStream(reader io.ReadCloser) *deepseekStream {
	return &deepseekStream{reader: reader, parser: streaming.NewSSEParser(reader)}
}

func (s *deepseekStream) Read(p []byte) (n int, err error)  { return s.reader.Read(p) }
func (s *deepseekStream) Close() error                      { return s.reader.Close() }
func (s *deepseekStream) Next() (*provider.StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}
	if streaming.IsStreamDone(event) {
		s.err = io.EOF
		return nil, io.EOF
	}
	var chunkData deepseekStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		return nil, fmt.Errorf("failed to parse stream chunk: %w", err)
	}
	if len(chunkData.Choices) > 0 {
		choice := chunkData.Choices[0]
		if choice.Delta.Content != "" {
			return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: choice.Delta.Content}, nil
		}
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
		if choice.FinishReason != "" {
			return &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: convertFinishReason(choice.FinishReason)}, nil
		}
	}
	return s.Next()
}
func (s *deepseekStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

package cohere

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for Cohere
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Cohere language model
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
	return "cohere"
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
	return false
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody := m.buildRequestBody(opts)
	var response cohereResponse
	err := m.provider.client.PostJSON(ctx, "/v1/chat", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	return m.convertResponse(response), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody := m.buildRequestBody(opts)
	reqBody["stream"] = true
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/chat",
		Body:   reqBody,
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	return newCohereStream(httpResp.Body), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) map[string]interface{} {
	body := map[string]interface{}{"model": m.modelID}
	if opts.Prompt.IsSimple() {
		body["message"] = opts.Prompt.Text
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	return body
}

func (m *LanguageModel) convertResponse(response cohereResponse) *types.GenerateResult {
	return &types.GenerateResult{
		Text:         response.Text,
		FinishReason: types.FinishReasonStop,
		Usage:        convertCohereUsage(response.Meta.Tokens),
		RawResponse:  response,
	}
}

func convertCohereUsage(tokens cohereTokens) types.Usage {
	inputTokens := int64(tokens.InputTokens)
	outputTokens := int64(tokens.OutputTokens)
	totalTokens := inputTokens + outputTokens
	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}
	result.InputDetails = &types.InputTokenDetails{
		NoCacheTokens:    &inputTokens,
		CacheReadTokens:  nil,
		CacheWriteTokens: nil,
	}
	result.OutputDetails = &types.OutputTokenDetails{
		TextTokens:      &outputTokens,
		ReasoningTokens: nil,
	}
	result.Raw = map[string]interface{}{
		"input_tokens":  tokens.InputTokens,
		"output_tokens": tokens.OutputTokens,
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
}

type cohereResponse struct {
	Text string `json:"text"`
	Meta struct {
		Tokens cohereTokens `json:"tokens"`
	} `json:"meta"`
}

type cohereTokens struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type cohereStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

func newCohereStream(reader io.ReadCloser) *cohereStream {
	return &cohereStream{reader: reader, parser: streaming.NewSSEParser(reader)}
}

func (s *cohereStream) Read(p []byte) (n int, err error)  { return s.reader.Read(p) }
func (s *cohereStream) Close() error                      { return s.reader.Close() }
func (s *cohereStream) Next() (*provider.StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}
	var chunkData struct {
		EventType string `json:"event_type"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		return nil, err
	}
	if chunkData.EventType == "text-generation" && chunkData.Text != "" {
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: chunkData.Text}, nil
	}
	if chunkData.EventType == "stream-end" {
		return &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}, nil
	}
	return s.Next()
}
func (s *cohereStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

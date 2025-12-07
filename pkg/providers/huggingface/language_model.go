package huggingface

import (
	"context"
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LanguageModel implements the provider.LanguageModel interface for Hugging Face
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Hugging Face language model
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
	return "huggingface"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return false
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return false
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Some models like LLaVA support images, but we'll default to false
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody := m.buildRequestBody(opts)

	path := fmt.Sprintf("/models/%s", m.modelID)
	resp, err := m.provider.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("huggingface", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Hugging Face API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return m.convertResponse(resp.Body)
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Hugging Face Inference API doesn't have native streaming for all models
	// We'll simulate it by chunking the response
	result, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}

	stream := &huggingfaceStream{
		result:   result,
		position: 0,
		done:     false,
	}

	return stream, nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) map[string]interface{} {
	// Build prompt from messages
	var promptText string
	if opts.Prompt.IsMessages() {
		for _, msg := range opts.Prompt.Messages {
			content := ""
			for _, c := range msg.Content {
				if tc, ok := c.(types.TextContent); ok {
					content += tc.Text
				}
			}

			if msg.Role == "system" {
				promptText += fmt.Sprintf("System: %s\n", content)
			} else if msg.Role == "user" {
				promptText += fmt.Sprintf("User: %s\n", content)
			} else if msg.Role == "assistant" {
				promptText += fmt.Sprintf("Assistant: %s\n", content)
			}
		}
		promptText += "Assistant: "
	} else if opts.Prompt.IsSimple() {
		promptText = opts.Prompt.Text
	}

	reqBody := map[string]interface{}{
		"inputs": promptText,
	}

	parameters := make(map[string]interface{})

	if opts.Temperature != nil {
		parameters["temperature"] = *opts.Temperature
	}

	if opts.MaxTokens != nil {
		parameters["max_new_tokens"] = *opts.MaxTokens
	}

	if opts.TopP != nil {
		parameters["top_p"] = *opts.TopP
	}

	if opts.TopK != nil {
		parameters["top_k"] = *opts.TopK
	}

	if len(parameters) > 0 {
		reqBody["parameters"] = parameters
	}

	return reqBody
}

func (m *LanguageModel) convertResponse(body []byte) (*types.GenerateResult, error) {
	// Hugging Face returns different formats depending on the model
	// Try to parse as array first (most common format)
	var responses []hfTextGenerationResponse
	if err := json.Unmarshal(body, &responses); err == nil && len(responses) > 0 {
		return &types.GenerateResult{
			Text:         responses[0].GeneratedText,
			FinishReason: types.FinishReasonStop,
			Usage: types.Usage{
				InputTokens:  0, // HF doesn't return token counts
				OutputTokens: 0,
				TotalTokens:  0,
			},
		}, nil
	}

	// Try single object format
	var response hfTextGenerationResponse
	if err := json.Unmarshal(body, &response); err == nil && response.GeneratedText != "" {
		return &types.GenerateResult{
			Text:         response.GeneratedText,
			FinishReason: types.FinishReasonStop,
			Usage: types.Usage{
				InputTokens:  0,
				OutputTokens: 0,
				TotalTokens:  0,
			},
		}, nil
	}

	// Try error format
	var errorResp hfErrorResponse
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != "" {
		return nil, fmt.Errorf("Hugging Face API error: %s", errorResp.Error)
	}

	return nil, fmt.Errorf("unexpected response format from Hugging Face: %s", string(body))
}

type hfTextGenerationResponse struct {
	GeneratedText string `json:"generated_text"`
}

type hfErrorResponse struct {
	Error string `json:"error"`
}

type huggingfaceStream struct {
	result   *types.GenerateResult
	position int
	done     bool
}

func (s *huggingfaceStream) Next() (*provider.StreamChunk, error) {
	if s.done {
		return nil, fmt.Errorf("stream exhausted")
	}

	// Emit text in chunks
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

func (s *huggingfaceStream) Err() error {
	return nil
}

func (s *huggingfaceStream) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("not implemented")
}

func (s *huggingfaceStream) Close() error {
	s.done = true
	return nil
}

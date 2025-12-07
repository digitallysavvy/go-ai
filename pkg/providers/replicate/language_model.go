package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LanguageModel implements the provider.LanguageModel interface for Replicate
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Replicate language model
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
	return "replicate"
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
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody := m.buildRequestBody(opts)

	// Create prediction
	resp, err := m.provider.client.Post(ctx, "/predictions", reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("replicate", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return nil, fmt.Errorf("Replicate API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var prediction replicatePrediction
	if err := json.Unmarshal(resp.Body, &prediction); err != nil {
		return nil, fmt.Errorf("failed to decode prediction response: %w", err)
	}

	// Poll for completion
	prediction, err = m.pollPrediction(ctx, prediction.ID)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(prediction), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Replicate supports streaming, but for simplicity we simulate it
	result, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}

	stream := &replicateStream{
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

	input := map[string]interface{}{
		"prompt": promptText,
	}

	if opts.Temperature != nil {
		input["temperature"] = *opts.Temperature
	}

	if opts.MaxTokens != nil {
		input["max_tokens"] = *opts.MaxTokens
	}

	if opts.TopP != nil {
		input["top_p"] = *opts.TopP
	}

	return map[string]interface{}{
		"version": m.modelID,
		"input":   input,
	}
}

func (m *LanguageModel) pollPrediction(ctx context.Context, predictionID string) (replicatePrediction, error) {
	maxAttempts := 60
	pollInterval := 2 * time.Second

	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return replicatePrediction{}, ctx.Err()
		default:
		}

		resp, err := m.provider.client.Get(ctx, "/predictions/"+predictionID)
		if err != nil {
			return replicatePrediction{}, err
		}

		var prediction replicatePrediction
		if err := json.Unmarshal(resp.Body, &prediction); err != nil {
			return replicatePrediction{}, fmt.Errorf("failed to decode prediction: %w", err)
		}

		if prediction.Status == "succeeded" {
			return prediction, nil
		}

		if prediction.Status == "failed" || prediction.Status == "canceled" {
			return replicatePrediction{}, fmt.Errorf("prediction %s: %s", prediction.Status, prediction.Error)
		}

		time.Sleep(pollInterval)
	}

	return replicatePrediction{}, fmt.Errorf("prediction timed out after %d attempts", maxAttempts)
}

func (m *LanguageModel) convertResponse(prediction replicatePrediction) *types.GenerateResult {
	var text string

	// Output can be a string or array of strings
	switch v := prediction.Output.(type) {
	case string:
		text = v
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				text += str
			}
		}
	}

	return &types.GenerateResult{
		Text:         text,
		FinishReason: types.FinishReasonStop,
		Usage: types.Usage{
			InputTokens:  0,
			OutputTokens: 0,
			TotalTokens:  0,
		},
	}
}

type replicatePrediction struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output interface{} `json:"output"`
	Error  string      `json:"error"`
}

type replicateStream struct {
	result   *types.GenerateResult
	position int
	done     bool
}

func (s *replicateStream) Next() (*provider.StreamChunk, error) {
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

func (s *replicateStream) Err() error {
	return nil
}

func (s *replicateStream) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("not implemented")
}

func (s *replicateStream) Close() error {
	s.done = true
	return nil
}

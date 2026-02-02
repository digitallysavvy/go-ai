package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for AWS Bedrock
type EmbeddingModel struct {
	provider *Provider
	modelID  string
	options  *EmbeddingOptions
}

// NewEmbeddingModel creates a new AWS Bedrock embedding model
func NewEmbeddingModel(provider *Provider, modelID string, options ...*EmbeddingOptions) *EmbeddingModel {
	var opts *EmbeddingOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
		options:  opts,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return "aws-bedrock"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Bedrock only supports 1 embedding per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 1
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	// Determine model type and construct request body accordingly
	var reqBody map[string]interface{}

	// Check if this is a Cohere model
	if len(m.modelID) >= 6 && m.modelID[:6] == "cohere" {
		// Validate Cohere options if provided
		if m.options != nil && m.options.CohereOptions != nil {
			if err := m.options.CohereOptions.Validate(); err != nil {
				return nil, err
			}
		}

		// Build Cohere request
		cohereOpts := DefaultCohereEmbeddingOptions()
		if m.options != nil && m.options.CohereOptions != nil {
			cohereOpts = *m.options.CohereOptions
		}

		reqBody = map[string]interface{}{
			"texts":      []string{input},
			"input_type": string(cohereOpts.InputType),
		}

		if cohereOpts.OutputDimension != nil {
			reqBody["output_dimension"] = int(*cohereOpts.OutputDimension)
		}

		if cohereOpts.Truncate != "" {
			reqBody["truncate"] = string(cohereOpts.Truncate)
		}
	} else {
		// Titan or other models
		reqBody = map[string]interface{}{
			"inputText": input,
		}

		// Add Titan-specific options if provided
		if m.options != nil && m.options.TitanOptions != nil {
			if m.options.TitanOptions.Dimensions != nil {
				reqBody["dimensions"] = *m.options.TitanOptions.Dimensions
			}
			if m.options.TitanOptions.Normalize != nil {
				reqBody["normalize"] = *m.options.TitanOptions.Normalize
			}
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("/model/%s/invoke", m.modelID)
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

	// Parse response based on model type
	var embedding []float64
	var inputTokens int

	if len(m.modelID) >= 6 && m.modelID[:6] == "cohere" {
		// Cohere response format
		var cohereResp struct {
			Embeddings [][]float64 `json:"embeddings"`
		}

		if err := json.Unmarshal(respBody, &cohereResp); err != nil {
			return nil, fmt.Errorf("failed to decode Cohere response: %w", err)
		}

		if len(cohereResp.Embeddings) == 0 {
			return nil, fmt.Errorf("no embeddings in response")
		}

		embedding = cohereResp.Embeddings[0]
		inputTokens = len(input) / 4 // Approximate
	} else {
		// Titan response format
		var titanResp struct {
			Embedding []float64 `json:"embedding"`
		}

		if err := json.Unmarshal(respBody, &titanResp); err != nil {
			return nil, fmt.Errorf("failed to decode Titan response: %w", err)
		}

		embedding = titanResp.Embedding
		inputTokens = len(input) / 4 // Approximate
	}

	return &types.EmbeddingResult{
		Embedding: embedding,
		Usage: types.EmbeddingUsage{
			InputTokens: inputTokens,
			TotalTokens: inputTokens,
		},
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	var embeddings [][]float64
	var totalTokens int

	// Process each input individually
	// Bedrock embeddings typically don't support batch processing
	for _, input := range inputs {
		result, err := m.DoEmbed(ctx, input)
		if err != nil {
			return nil, err
		}

		embeddings = append(embeddings, result.Embedding)
		totalTokens += result.Usage.InputTokens
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: totalTokens,
			TotalTokens: totalTokens,
		},
	}, nil
}

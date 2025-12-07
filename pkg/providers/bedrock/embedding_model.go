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
}

// NewEmbeddingModel creates a new AWS Bedrock embedding model
func NewEmbeddingModel(provider *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
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
	reqBody := map[string]interface{}{
		"inputText": input,
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

	var embeddingResp struct {
		Embedding []float64 `json:"embedding"`
	}

	if err := json.Unmarshal(respBody, &embeddingResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &types.EmbeddingResult{
		Embedding: embeddingResp.Embedding,
		Usage: types.EmbeddingUsage{
			InputTokens: len(input) / 4, // Approximate
			TotalTokens: len(input) / 4,
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

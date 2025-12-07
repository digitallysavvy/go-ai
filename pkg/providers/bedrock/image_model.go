package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for AWS Bedrock
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new AWS Bedrock image generation model
func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "aws-bedrock"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

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

	return m.convertResponse(respBody)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	// Stable Diffusion on Bedrock uses specific format
	textPrompts := []map[string]interface{}{
		{
			"text":   opts.Prompt,
			"weight": 1.0,
		},
	}

	reqBody := map[string]interface{}{
		"text_prompts": textPrompts,
	}

	if opts.Size != "" {
		var width, height int
		fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			reqBody["width"] = width
			reqBody["height"] = height
		}
	}

	if opts.N != nil {
		reqBody["samples"] = *opts.N
	}

	return reqBody
}

func (m *ImageModel) convertResponse(body []byte) (*types.ImageResult, error) {
	var response struct {
		Result      string `json:"result"`
		Artifacts   []struct {
			Base64       string `json:"base64"`
			FinishReason string `json:"finishReason"`
		} `json:"artifacts"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Artifacts) == 0 {
		return nil, fmt.Errorf("no images generated")
	}

	// Bedrock returns base64 encoded images
	return &types.ImageResult{
		Image:    []byte(response.Artifacts[0].Base64),
		MimeType: "image/png",
		URL:      "",
		Usage:    types.ImageUsage{},
	}, nil
}

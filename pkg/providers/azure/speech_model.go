package azure

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechModel implements the provider.SpeechModel interface for Azure OpenAI
type SpeechModel struct {
	provider     *Provider
	deploymentID string
}

// NewSpeechModel creates a new Azure OpenAI speech synthesis model
func NewSpeechModel(provider *Provider, deploymentID string) *SpeechModel {
	return &SpeechModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "azure.speech"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *SpeechModel) ModelID() string {
	return m.deploymentID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody := m.buildRequestBody(opts)

	// Azure OpenAI speech generation endpoint
	path := m.provider.endpointPath(m.deploymentID, "/audio/speech")

	resp, err := m.provider.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("azure OpenAI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	// The response is the raw audio bytes
	return &types.SpeechResult{
		Audio:    resp.Body,
		MimeType: "audio/mpeg",
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"input": opts.Text,
		"model": m.deploymentID,
	}

	if opts.Voice != "" {
		reqBody["voice"] = opts.Voice
	} else {
		reqBody["voice"] = "alloy" // Default voice
	}

	if opts.Speed != nil {
		reqBody["speed"] = *opts.Speed
	}

	// Default to mp3 format
	reqBody["response_format"] = "mp3"

	return reqBody
}

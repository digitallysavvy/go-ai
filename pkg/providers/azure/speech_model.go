package azure

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
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
	return "v4"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "azure.speech"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *SpeechModel) ModelID() string {
	return m.deploymentID
}

// DoGenerate performs speech synthesis through the OpenAI speech model
// configured for this deployment (TS wraps OpenAISpeechModel), so
// providerOptions.openai speed/instructions, warnings and response metadata
// match.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	return openai.NewSpeechModel(m.provider.openaiDelegate(m.deploymentID), m.deploymentID).DoGenerate(ctx, opts)
}

package openai

import (
	"context"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechModel implements the provider.SpeechModel interface for OpenAI TTS
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// NewSpeechModel creates a new OpenAI speech synthesis model
func NewSpeechModel(provider *Provider, modelID string) *SpeechModel {
	return &SpeechModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "openai"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs speech synthesis
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody := m.buildRequestBody(opts)

	resp, err := m.provider.client.Post(ctx, "/v1/audio/speech", reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("openai", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenAI TTS API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return &types.SpeechResult{
		Audio:    resp.Body,
		MimeType: "audio/mpeg",
		Usage: types.SpeechUsage{
			CharacterCount: len(opts.Text),
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"model": m.modelID,
		"input": opts.Text,
	}
	if opts.Voice != "" {
		body["voice"] = opts.Voice
	} else {
		body["voice"] = "alloy"
	}
	if opts.Speed != nil {
		body["speed"] = *opts.Speed
	}
	return body
}
